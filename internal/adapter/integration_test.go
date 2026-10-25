package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Exercises the public session lifecycle, real MAVLink UDP transport, SQLite,
// HTTP contracts, offline restart, map feed and the final audit report together.
func TestFlightSessionIntegration(t *testing.T) {
	var offline atomic.Bool
	offline.Store(true)
	var mu sync.Mutex
	archived := map[string]archiveBatch{}
	logs := []map[string]any{}
	posts := 0
	fb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if offline.Load() {
			http.Error(w, "offline", 503)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fb-secret" {
			http.Error(w, "auth", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/ping" || r.URL.Path == "/ping/":
			io.WriteString(w, `{"status":"ok"}`)
		case r.URL.Path == "/flight_declaration_ops/flight_declaration":
			if r.URL.Query().Get("start_date") == "" {
				t.Error("historical declaration window missing")
			}
			fmt.Fprintf(w, `{"count":1,"results":[{"id":%q,"state":2}]}`, assignment(1).DeclarationID)
		case r.URL.Path == "/geo_fence_ops/geo_fence":
			io.WriteString(w, `{"count":1,"results":[{"id":"fence","name":"Airport","raw_geo_fence":{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"upperLimit":120},"geometry":{"type":"Polygon","coordinates":[[[19,47],[20,47],[20,48],[19,47]]]}}]}}]}`)
		case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/adapter_ops/sessions/"):
			var b archiveBatch
			if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			mu.Lock()
			archived[b.Session.ID] = b
			mu.Unlock()
			count := int64(len(b.Samples))
			complete := b.Session.FinalSampleCount != nil && *b.Session.FinalSampleCount == count
			fmt.Fprintf(w, `{"id":%q,"revision":%d,"archive":{"complete":%t,"receivedSampleCount":%d,"finalSampleCount":%s}}`, b.Session.ID, b.Session.Revision, complete, count, func() string {
				if b.Session.FinalSampleCount == nil {
					return "null"
				}
				return fmt.Sprint(*b.Session.FinalSampleCount)
			}())
		default:
			http.NotFound(w, r)
		}
	}))
	defer fb.Close()
	ab := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if offline.Load() {
			http.Error(w, "offline", 503)
			return
		}
		if r.Header.Get("Authorization") != "Bearer ab-secret" {
			http.Error(w, "auth", 401)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/registry/aircraft/" {
			io.WriteString(w, `[]`)
			return
		}
		if r.URL.Path != "/gcs/flight-logs" {
			http.NotFound(w, r)
			return
		}
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(logs)
			return
		}
		var log map[string]any
		if err := json.NewDecoder(r.Body).Decode(&log); err != nil {
			t.Error(err)
			return
		}
		posts++
		log["id"] = newID()
		logs = append(logs, log)
		// Simulate an ambiguous failure after the first durable remote acceptance.
		if posts == 1 {
			http.Error(w, "response lost", 503)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(log)
	}))
	defer ab.Close()
	c := DefaultConfig()
	c.BaseURL = fb.URL
	c.AuthMode = "bearer"
	c.Token = "fb-secret"
	c.AerobridgeEnabled = true
	c.AerobridgeURL = ab.URL
	c.AerobridgeToken = "ab-secret"
	c.SystemID = 0
	c.Address = freeUDP(t)
	s := testService(t, c)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	a := assignment(1)
	a.AerobridgeOperationID = "00000000-0000-0000-0000-000000000099"
	first, err := s.BeginSession(a)
	if err != nil {
		t.Fatal(err)
	}
	a.SystemID = 2
	a.VehicleID = "vehicle-2"
	a.Pilot.Name = "Other Pilot"
	second, err := s.BeginSession(a)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("udp", c.Address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, system := range []byte{1, 2} {
		if _, err = conn.Write(packets(t, system, heartbeat())); err != nil {
			t.Fatal(err)
		}
		if _, err = conn.Write(packets(t, system, gps())); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, func() bool {
		list, e := s.records.list(10)
		return e == nil && len(list) == 2 && list[0].Samples == 1 && list[1].Samples == 1
	})
	for _, id := range []string{first.ID, second.ID} {
		if err = s.EndSession(id); err != nil {
			t.Fatal(err)
		}
	}
	path := s.path
	s.Close()
	// Reconstruct with no in-memory vehicle or delivery state.
	s = &Service{path: path, config: c, vehicles: map[byte]Vehicle{}}
	defer s.Close()
	if err = s.ensureRecords(); err != nil {
		t.Fatal(err)
	}
	offline.Store(false)
	for i := 0; i < 5; i++ {
		if err = s.Synchronize(); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.records.list(10)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range list {
		if record.BlenderPending || record.AerobridgePending {
			t.Fatalf("delivery remains pending: %+v", record)
		}
	}
	mu.Lock()
	if len(archived) != 2 || posts < 2 || len(logs) != posts {
		t.Errorf("archive=%d posts=%d logs=%d", len(archived), posts, len(logs))
	}
	keys := map[string]bool{}
	for _, log := range logs {
		key := log["raw_log"].(map[string]any)["adapterBatchKey"].(string)
		if keys[key] {
			t.Error("duplicate Aerobridge batch")
		}
		keys[key] = true
	}
	mu.Unlock()
	bridge, err := s.startBridge("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.close()
	response, err := http.Get(bridge.url + "/v1/restrictions")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var restrictions RestrictionMap
	if err = json.Unmarshal(raw, &restrictions); err != nil || restrictions.Stale || len(restrictions.Features) != 1 {
		t.Fatalf("map: %s %v", raw, err)
	}
	if bytes.Contains(raw, []byte("secret")) || bytes.Contains(raw, []byte("Pilot")) {
		t.Fatal("map feed leaked private data")
	}
	for _, id := range []string{first.ID, second.ID} {
		export, e := s.ExportSession(id)
		if e != nil {
			t.Fatal(e)
		}
		raw, e = os.ReadFile(export)
		if e != nil {
			t.Fatal(e)
		}
		var report struct {
			Session      Session           `json:"session"`
			Samples      []Sample          `json:"samples"`
			Declarations []json.RawMessage `json:"declarationHistory"`
			Audit        []any             `json:"audit"`
		}
		if e = json.Unmarshal(raw, &report); e != nil {
			t.Fatal(e)
		}
		if len(report.Samples) != 1 || len(report.Declarations) != 1 || len(report.Audit) != 2 || int(report.Samples[0].Vehicle.SystemID) != report.Session.Assignment.SystemID || report.Session.Assignment.Pilot.License != "TEST-LICENCE" {
			t.Fatalf("invalid report %s", raw)
		}
		if bytes.Contains(raw, []byte("secret")) {
			t.Fatal("report contains credential")
		}
	}
}

// Used by scripts/test-flight-blender.sh against the real Python application.
func TestFlightBlenderSessionContract(t *testing.T) {
	base := os.Getenv("UTM_TEST_FLIGHT_BLENDER_URL")
	if base == "" {
		t.Skip("run scripts/test-flight-blender.sh for the real application contract test")
	}
	c := DefaultConfig()
	c.BaseURL = base
	c.AuthMode = "bearer"
	c.Token = os.Getenv("UTM_TEST_FLIGHT_BLENDER_TOKEN")
	r, err := openRecords(filepath.Join(t.TempDir(), "records.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.db.Close()
	session, err := r.begin(assignment(1), c)
	if err != nil {
		t.Fatal(err)
	}
	v := Vehicle{SystemID: 1, GPSAt: time.Now().UTC(), Fix: 3, Latitude: f64(47.5), Longitude: f64(19), AltitudeMSL: f64(120), AltitudeWGS84: f64(164), Speed: f64(2.1)}
	if err = r.append(v); err != nil {
		t.Fatal(err)
	}
	if err = r.end(session.ID); err != nil {
		t.Fatal(err)
	}
	batch, err := r.batch(session.ID, "blender")
	if err != nil {
		t.Fatal(err)
	}
	b := NewBlender(c)
	ctx := context.Background()
	route := "/adapter_ops/sessions/" + session.ID
	for i := 0; i < 2; i++ {
		if _, err = b.request(ctx, "PUT", route, batch, true); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := b.request(ctx, "GET", route, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	var got SessionBatch
	if err = json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Samples) != 1 || got.Session.EndedAt == nil || got.Session.Assignment.Pilot != batch.Session.Assignment.Pilot || *got.Samples[0].Vehicle.Speed != 2.1 {
		t.Fatalf("round trip failed: %s", raw)
	}
	if err = r.ack(batch, "blender"); err != nil {
		t.Fatal(err)
	}
}

// Runs the public adapter workflow all the way from a UDP frame to the actual
// Flight Blender database and the exported report. No HTTP service is mocked.
func TestFlightBlenderWorkflow(t *testing.T) {
	base := os.Getenv("UTM_TEST_FLIGHT_BLENDER_URL")
	if base == "" {
		t.Skip("run scripts/test-flight-blender.sh for the real application workflow")
	}
	c := DefaultConfig()
	c.BaseURL, c.AuthMode, c.Token = base, "bearer", os.Getenv("UTM_TEST_FLIGHT_BLENDER_TOKEN")
	c.Address, c.Source = freeUDP(t), "direct"
	s := testService(t, c)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	session, err := s.BeginSession(assignment(1))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("udp", c.Address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, frame := range [][]byte{packets(t, 1, heartbeat()), packets(t, 1, gps())} {
		if _, err = conn.Write(frame); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, func() bool {
		list, e := s.records.list(10)
		return e == nil && len(list) == 1 && list[0].Samples == 1
	})
	if err = s.EndSession(session.ID); err != nil {
		t.Fatal(err)
	}
	path := s.path
	s.Close()
	s = &Service{path: path, config: c, vehicles: map[byte]Vehicle{}}
	defer s.Close()
	// An initial in-flight empty batch may have been persisted before the GPS.
	for i := 0; i < 3; i++ {
		if err = s.Synchronize(); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.records.list(10)
	if err != nil || len(list) != 1 || list[0].BlenderPending {
		t.Fatalf("archive not acknowledged: %+v %v", list, err)
	}
	raw, err := NewBlender(c).request(context.Background(), "GET", "/adapter_ops/sessions/"+session.ID, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	var remote archiveBatch
	if err = json.Unmarshal(raw, &remote); err != nil {
		t.Fatal(err)
	}
	exported, err := s.ExportSession(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(exported)
	if err != nil {
		t.Fatal(err)
	}
	var report SessionBatch
	if err = json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if len(remote.Samples) != 1 || remote.Session.EndedAt == nil || remote.Session.Assignment.Pilot != session.Assignment.Pilot || remote.Samples[0].SourceData.Speed == nil || *remote.Samples[0].SourceData.Speed != 2.1 {
		t.Fatalf("actual server lost session data: %+v", remote)
	}
	remoteJSON, _ := json.Marshal(remote)
	expectedArchive, err := s.records.archive(report)
	if err != nil {
		t.Fatal(err)
	}
	// Compare the common metadata and original source evidence; the server also fills optional normalized fields.
	for i := range remote.Samples {
		remote.Samples[i].Position = expectedArchive.Samples[i].Position
	}
	remote.Archive = nil
	remoteJSON, _ = json.Marshal(remote)
	reportJSON, _ := json.Marshal(expectedArchive)
	if !bytes.Equal(remoteJSON, reportJSON) {
		t.Fatalf("server and exported record differ:\n%s\n%s", remoteJSON, reportJSON)
	}
}
