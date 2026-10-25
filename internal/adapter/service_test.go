package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/bluenviron/gomavlib/v3/pkg/dialect"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/ardupilotmega"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"
	"github.com/bluenviron/gomavlib/v3/pkg/frame"
	"github.com/bluenviron/gomavlib/v3/pkg/message"
	"github.com/bluenviron/gomavlib/v3/pkg/streamwriter"
)

func packets(t *testing.T, system byte, msgs ...message.Message) []byte {
	t.Helper()
	var buf bytes.Buffer
	dr := &dialect.ReadWriter{Dialect: ardupilotmega.Dialect}
	if e := dr.Initialize(); e != nil {
		t.Fatal(e)
	}
	fw := &frame.Writer{ByteWriter: &buf, DialectRW: dr}
	if e := fw.Initialize(); e != nil {
		t.Fatal(e)
	}
	w := &streamwriter.Writer{FrameWriter: fw, Version: streamwriter.V2, SystemID: system, ComponentID: 1}
	if e := w.Initialize(); e != nil {
		t.Fatal(e)
	}
	for _, m := range msgs {
		if e := w.Write(m); e != nil {
			t.Fatal(e)
		}
	}
	return buf.Bytes()
}
func heartbeat() *common.MessageHeartbeat {
	return &common.MessageHeartbeat{Type: common.MAV_TYPE_QUADROTOR, MavlinkVersion: 3}
}
func gps() *common.MessageGpsRawInt {
	return &common.MessageGpsRawInt{FixType: common.GPS_FIX_TYPE_3D_FIX, Lat: 474979000, Lon: 190402000, Alt: 120000, AltEllipsoid: 164000, Vel: 210, Cog: 9000, SatellitesVisible: 14}
}
func testService(t *testing.T, c Config) *Service {
	t.Helper()
	// Connection integration tests must never change installed ground stations.
	c.AutoConfigureGCS = false
	c.BridgeAddress = "127.0.0.1:0"
	s := &Service{config: c, path: filepath.Join(t.TempDir(), "product.json"), vehicles: map[byte]Vehicle{}}
	s.resetLinks()
	s.state.Events = []Activity{}
	t.Cleanup(s.Close)
	return s
}
func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}

func TestUDPParserDiagnostics(t *testing.T) {
	c := DefaultConfig()
	c.Address = freeUDP(t)
	c.BaseURL = "http://127.0.0.1:1"
	s := testService(t, c)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("udp", c.Address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// SITL's console emits these progress markers alongside MAVLink on serial0.
	conn.Write([]byte("*"))
	conn.Write(packets(t, 1, heartbeat()))
	conn.Write(packets(t, 1, gps()))
	waitFor(t, func() bool {
		snap := s.GetSnapshot()
		return len(snap.Vehicles) == 1 && !snap.Vehicles[0].GPSAt.IsZero()
	})
	snap := s.GetSnapshot()
	if snap.ParseErrors != 1 || !strings.Contains(snap.LastParseError, "console output") || !strings.Contains(snap.LastParseError, "invalid magic byte: 2a") {
		t.Fatalf("missing console diagnostic: %d errors, %q", snap.ParseErrors, snap.LastParseError)
	}
	conn.Write([]byte("Init Gyro"))
	conn.Write(packets(t, 1, heartbeat()))
	waitFor(t, func() bool { return strings.Contains(s.GetSnapshot().LastParseError, "skipped 8 bytes") })
	if detail := s.GetSnapshot().LastParseError; !strings.Contains(detail, "console text or multiple frames") {
		t.Fatalf("missing unread-data diagnostic: %q", detail)
	}
	snap = s.GetSnapshot()
	corrupted := packets(t, 1, gps())
	corrupted[len(corrupted)-1] ^= 0xff
	conn.Write(corrupted)
	waitFor(t, func() bool { return s.GetSnapshot().ParseErrors > snap.ParseErrors })
	if detail := s.GetSnapshot().LastParseError; !strings.Contains(detail, "wrong checksum") || strings.Contains(detail, "console") {
		t.Fatalf("checksum failure misclassified: %q", detail)
	}
	frames := s.GetSnapshot().Frames
	conn.Write(packets(t, 1, gps()))
	waitFor(t, func() bool { return s.GetSnapshot().Frames > frames })
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if snap := s.GetSnapshot(); snap.ParseErrors != 0 || snap.LastParseError != "" {
		t.Fatalf("restart retained stale parse diagnostics: %+v", snap)
	}
}
func freeUDP(t *testing.T) string {
	t.Helper()
	c, e := net.ListenPacket("udp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	a := c.LocalAddr().String()
	c.Close()
	return a
}
func TestUDPToFlightBlender(t *testing.T) {
	delivered := make(chan map[string]any, 10)
	var tokens atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/oauth" {
			id, secret, ok := r.BasicAuth()
			if !ok || id != "client" || secret != "private" {
				t.Error("missing OAuth basic authentication")
			}
			tokens.Add(1)
			io.WriteString(w, `{"access_token":"test-token","expires_in":3600}`)
			return
		}
		if r.URL.Path == "/ping" {
			io.WriteString(w, `{"message":"pong"}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing bearer token")
			w.WriteHeader(401)
			return
		}
		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/flight_declaration_ops/flight_declaration") {
			if r.URL.Query().Get("page_size") == "100" {
				io.WriteString(w, `{"count":0,"results":[]}`)
			} else {
				io.WriteString(w, `{"id":"00000000-0000-0000-0000-000000000001"}`)
			}
			return
		}
		if r.URL.Path == "/geo_fence_ops/geo_fence" {
			io.WriteString(w, `{"count":0,"results":[]}`)
			return
		}
		if r.Method != "PUT" || r.URL.Path != "/flight_stream/set_telemetry" {
			t.Errorf("unexpected route %s %s", r.Method, r.URL)
			w.WriteHeader(404)
			return
		}
		var body map[string]any
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		delivered <- body
		w.WriteHeader(201)
		io.WriteString(w, `{"message":"Telemetry data successfully submitted"}`)
	}))
	defer server.Close()
	c := DefaultConfig()
	c.BaseURL = server.URL
	c.AuthMode = "oauth"
	c.TokenURL = server.URL + "/oauth"
	c.ClientID = "client"
	c.ClientSecret = "private"
	c.Address = freeUDP(t)
	c.SystemID = 1
	c.Publish = true
	c.DeclarationID = "00000000-0000-0000-0000-000000000001"
	c.VehicleID = "TEST-001"
	s := testService(t, c)
	if e := s.Start(); e != nil {
		t.Fatal(e)
	}
	conn, e := net.Dial("udp", c.Address)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	// A GCS heartbeat must not become a vehicle, and another system must be filtered.
	conn.Write(packets(t, 255, &common.MessageHeartbeat{Type: common.MAV_TYPE_GCS}))
	conn.Write(packets(t, 2, heartbeat()))
	conn.Write(packets(t, 2, gps()))
	corrupted := packets(t, 1, gps())
	corrupted[len(corrupted)-1] ^= 0xff
	conn.Write(corrupted)
	conn.Write(packets(t, 1, heartbeat()))
	conn.Write(packets(t, 1, gps()))
	waitFor(t, func() bool { return s.GetSnapshot().Sent > 0 })
	snap := s.GetSnapshot()
	if len(snap.Vehicles) != 1 || snap.Vehicles[0].SystemID != 1 {
		t.Fatalf("wrong vehicles: %+v", snap.Vehicles)
	}
	if snap.ParseErrors == 0 {
		t.Fatal("bad CRC was not rejected")
	}
	if snap.Config.ClientSecret != "" || !snap.Config.HasClientSecret {
		t.Fatal("credential leaked or presence missing")
	}
	if snap.Blender.State != "connected" || snap.Access.State != "connected" {
		t.Fatalf("bad status: %+v", snap)
	}
	body := <-delivered
	observation := body["observations"].([]any)[0].(map[string]any)
	state := observation["current_states"].([]any)[0].(map[string]any)
	position := state["position"].(map[string]any)
	if position["alt"] != float64(164) || position["lat"] != 47.4979 || state["speed"] != 2.1 {
		t.Fatalf("wrong conversion: %+v", state)
	}
	if observation["flight_details"].(map[string]any)["id"] != c.DeclarationID {
		t.Fatal("wrong declaration ID")
	}
	if tokens.Load() != 1 {
		t.Fatal("OAuth token was not cached")
	}
	s.Stop()
	if s.GetSnapshot().Running {
		t.Fatal("stop failed")
	}
	// The listen port must be released immediately after stopping.
	listener, e := net.ListenPacket("udp", c.Address)
	if e != nil {
		t.Fatal(e)
	}
	listener.Close()
}
func TestDirectTCP(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	c := DefaultConfig()
	c.Transport = "tcp"
	c.Source = "direct"
	c.Address = listener.Addr().String()
	c.BaseURL = "http://127.0.0.1:1"
	s := testService(t, c)
	if e = s.Start(); e != nil {
		t.Fatal(e)
	}
	conn, e := listener.Accept()
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	conn.Write(packets(t, 1, heartbeat(), gps()))
	waitFor(t, func() bool { return len(s.GetSnapshot().Vehicles) == 1 })
	if s.GetSnapshot().Telemetry.State != "connected" {
		t.Fatal("TCP telemetry not connected")
	}
}
func TestAltitudeAndFixGuards(t *testing.T) {
	s := testService(t, DefaultConfig())
	g := gps()
	g.AltEllipsoid = 0
	s.consume(&gomavlib.EventFrame{Frame: &frame.V2Frame{SystemID: 1, ComponentID: 1, Message: g}})
	if s.GetSnapshot().Vehicles[0].AltitudeWGS84 != nil {
		t.Fatal("MSL treated as WGS84")
	}
	g = gps()
	s.consume(&gomavlib.EventFrame{Frame: &frame.V1Frame{SystemID: 1, ComponentID: 1, Message: g}})
	if s.GetSnapshot().Vehicles[0].AltitudeWGS84 != nil {
		t.Fatal("v1 extension treated as WGS84")
	}
	g = gps()
	g.FixType = common.GPS_FIX_TYPE_NO_FIX
	s.consume(&gomavlib.EventFrame{Frame: &frame.V2Frame{SystemID: 1, ComponentID: 1, Message: g}})
	if s.GetSnapshot().Vehicles[0].AltitudeWGS84 != nil {
		t.Fatal("no-fix sample treated as valid GPS")
	}
	b := NewBlender(DefaultConfig())
	if b.Publish(context.Background(), Vehicle{}) == nil {
		t.Fatal("published incomplete GPS")
	}
}
func TestConfigDoesNotPersistCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "product.json")
	c := DefaultConfig()
	c.Token = "super-secret"
	c.ClientSecret = "another-secret"
	if e := SaveConfig(path, c); e != nil {
		t.Fatal(e)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("secret")) {
		t.Fatal("secret persisted")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows Chmod only controls the read-only attribute, not Unix permissions.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("wrong config permissions: got %04o, want 0600", info.Mode().Perm())
	}
	if c.Publish = true; c.Validate() == nil {
		t.Fatal("allowed publication without identity")
	}
}
func TestAuthFailuresAndRedirects(t *testing.T) {
	for _, status := range []int{401, 403, 500, 302} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "http://example.invalid")
				w.WriteHeader(status)
				io.WriteString(w, "secret-in-error-body")
			}))
			defer server.Close()
			c := DefaultConfig()
			c.BaseURL = server.URL
			c.Token = "private"
			err := NewBlender(c).CheckAccess(context.Background())
			if err == nil || strings.Contains(err.Error(), "secret-in-error-body") {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
}
