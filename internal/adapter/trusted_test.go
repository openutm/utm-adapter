package adapter

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/ardupilotmega"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"
)

const aircraftID = "10000000-0000-0000-0000-000000000001"
const pilotID = "20000000-0000-0000-0000-000000000001"
const operatorID = "30000000-0000-0000-0000-000000000001"
const orgID = "40000000-0000-0000-0000-000000000001"
const planID = "50000000-0000-0000-0000-000000000001"
const operationID = "60000000-0000-0000-0000-000000000001"

var testUID = [18]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18}

const controllerID = "0102030405060708090A0B0C0D0E0F101112"

type trustedRig struct {
	corrupt                         bool
	losePlanResponse                bool
	credentials                     []map[string]any
	s                               *Service
	mu                              sync.Mutex
	permission                      string
	pilotActive                     bool
	files                           map[string][]byte
	plans, operations, logs, signed []map[string]any
	posts                           map[string]int
	ftpWrites                       int
	drop                            bool
	failSign                        bool
}

func newTrustedRig(t *testing.T) *trustedRig {
	t.Helper()
	r := &trustedRig{permission: "granted", pilotActive: true, files: map[string][]byte{"trusted_flight/flight_log.json": []byte(`{"samples":[{"lat":47.5}]}`)}, posts: map[string]int{}}
	r.plans = []map[string]any{{"id": planID, "name": "Stored", "plan_file_json": map[string]any{"fileType": "Plan", "mission": map[string]any{"items": []any{}}}}}
	op := map[string]any{"id": operationID, "drone": aircraftID, "pilot": pilotID, "flight_plan": planID, "operator": operatorID, "organization": orgID}
	r.operations = []map[string]any{op}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path == "/ping/" {
			fmt.Fprint(w, `{"status":"ok"}`)
			return
		}
		if req.URL.Path == "/oauth/token/" {
			_ = req.ParseForm()
			if req.Form.Get("client_secret") != "secret" || req.Form.Get("grant_type") != "client_credentials" || req.Form.Get("audience") != "aerobridge" {
				http.Error(w, "bad OAuth", 400)
				return
			}
			fmt.Fprint(w, `{"access_token":"passport-token","expires_in":3600}`)
			return
		}
		if req.Header.Get("Authorization") != "Bearer passport-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		var output any
		switch req.URL.Path {
		case "/registry/aircraft/rfm/" + controllerID:
			output = map[string]any{"id": aircraftID, "status": 1, "flight_controller_id": controllerID}
		case "/registry/pilots/":
			output = []any{map[string]any{"id": pilotID, "is_active": r.pilotActive, "operator": operatorID}}
		case "/registry/aircraft-component/verify/component-1":
			output = map[string]any{"aerobridge_id": "component-1", "status": true}
		case "/pki/credentials/":
			if req.Method == "POST" {
				var v map[string]any
				_ = json.NewDecoder(req.Body).Decode(&v)
				v["id"] = newID()
				r.credentials = append(r.credentials, v)
				r.posts["credentials"]++
				output = v
			} else {
				output = r.credentials
			}
		case "/gcs/flight-plans":
			if req.Method == "POST" {
				var v map[string]any
				_ = json.NewDecoder(req.Body).Decode(&v)
				v["id"] = newID()
				r.plans = append(r.plans, v)
				r.posts["plans"]++
				if r.losePlanResponse {
					r.losePlanResponse = false
					http.Error(w, "lost response", 500)
					return
				}
				output = v
			} else {
				output = r.plans
			}
		case "/gcs/flight-operations":
			if req.Method == "POST" {
				var v map[string]any
				_ = json.NewDecoder(req.Body).Decode(&v)
				v["id"] = newID()
				r.operations = append(r.operations, v)
				r.posts["operations"]++
				output = v
			} else {
				output = r.operations
			}
		case "/gcs/flight-operations/" + operationID:
			output = op
		case "/gcs/flight-operations/" + operationID + "/permission":
			output = map[string]any{"status_code": r.permission, "operation": op, "token": map[string]any{"access_token": "permission-token"}, "geo_cage": []any{map[string]float64{"lat": 47, "lng": 19}, map[string]float64{"lat": 47.1, "lng": 19}, map[string]float64{"lat": 47, "lng": 19.1}}}
		case "/gcs/flight-plans/" + planID:
			output = r.plans[0]
		case "/gcs/flight-logs":
			if req.Method == "POST" {
				var v map[string]any
				_ = json.NewDecoder(req.Body).Decode(&v)
				v["id"] = newID()
				r.logs = append(r.logs, v)
				r.posts["logs"]++
				output = v
			} else {
				output = r.logs
			}
		case "/gcs/signed-flight-logs":
			output = r.signed
		default:
			if strings.HasPrefix(req.URL.Path, "/gcs/flight-logs/") && strings.HasSuffix(req.URL.Path, "/sign") {
				if r.failSign {
					http.Error(w, "upstream failure", 500)
					return
				}
				id := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/gcs/flight-logs/"), "/sign")
				output = map[string]any{"id": newID(), "raw_flight_log": id, "signed_log": map[string]any{"signature": "signed"}}
				r.signed = append(r.signed, output.(map[string]any))
				r.posts["sign"]++
			} else {
				http.NotFound(w, req)
				return
			}
		}
		if output == nil {
			output = []any{}
		}
		_ = json.NewEncoder(w).Encode(output)
	}))
	t.Cleanup(server.Close)
	c := DefaultConfig()
	c.Address = freeUDP(t)
	c.BaseURL = "http://127.0.0.1:1"
	s := testService(t, c)
	r.s = s
	tc := TrustedConfig{Enabled: true, ServerURL: server.URL, TimeoutSeconds: 10, FlightPassport: PassportConfig{URL: server.URL, TokenURL: "/oauth/token/", Audience: "aerobridge", ClientID: "client", ClientSecret: "secret"}, Credentials: TrustedCredentials{OrganizationID: orgID, OperatorID: operatorID}}
	s.trusted = &trustedModule{config: tc, jobs: map[string]TrustedJob{}}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	node := &gomavlib.Node{Endpoints: []gomavlib.EndpointConf{gomavlib.EndpointUDPClient{Address: c.Address}}, Dialect: ardupilotmega.Dialect, OutVersion: gomavlib.V2, OutSystemID: 1, HeartbeatDisable: true}
	if err := node.Initialize(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() { node.Close(); <-done })
	go func() {
		defer close(done)
		sessions := map[byte]string{}
		nextSession := byte(1)
		for ev := range node.Events() {
			frame, ok := ev.(*gomavlib.EventFrame)
			if !ok {
				continue
			}
			switch m := frame.Message().(type) {
			case *common.MessageCommandLong:
				_ = node.WriteMessageAll(&common.MessageAutopilotVersion{Uid2: testUID})
			case *common.MessageFileTransferProtocol:
				r.mu.Lock()
				p := m.Payload
				if r.drop {
					r.drop = false
					r.mu.Unlock()
					continue
				}
				var a [251]byte
				binary.LittleEndian.PutUint16(a[:2], binary.LittleEndian.Uint16(p[:2])+1)
				a[2] = p[2]
				a[3] = 128
				a[5] = p[3]
				copy(a[8:12], p[8:12])
				path := strings.TrimRight(string(p[12:12+int(p[4])]), "\x00")
				offset := int(binary.LittleEndian.Uint32(p[8:12]))
				nak := func(code byte) { a[3] = 129; a[4] = 1; a[12] = code }
				switch p[3] {
				case 9: // mkdir
				case 8:
					if _, ok := r.files[path]; !ok {
						nak(10)
					} else {
						delete(r.files, path)
					}
				case 6:
					nextSession++
					a[2] = nextSession
					sessions[nextSession] = path
					r.files[path] = nil
				case 4:
					if data, ok := r.files[path]; ok {
						nextSession++
						a[2] = nextSession
						sessions[nextSession] = path
						a[4] = 4
						binary.LittleEndian.PutUint32(a[12:16], uint32(len(data)))
					} else {
						nak(10)
					}
				case 7:
					path = sessions[p[2]]
					data := r.files[path]
					if offset > len(data) {
						nak(2)
					} else {
						if offset+int(p[4]) > len(data) {
							data = append(data, make([]byte, offset+int(p[4])-len(data))...)
						}
						copy(data[offset:], p[12:12+int(p[4])])
						r.files[path] = data
						r.ftpWrites++
					}
				case 5:
					data := r.files[sessions[p[2]]]
					if offset >= len(data) {
						nak(6)
					} else {
						n := min(int(p[4]), len(data)-offset)
						a[4] = byte(n)
						copy(a[12:], data[offset:offset+n])
						if r.corrupt {
							a[12] ^= 1
						}
					}
				case 1:
					delete(sessions, p[2])
				default:
					nak(7)
				}
				r.mu.Unlock()
				_ = node.WriteMessageAll(&common.MessageFileTransferProtocol{TargetSystem: 254, TargetComponent: 1, Payload: a})
			}
		}
	}()
	heartbeatDone := make(chan struct{})
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop); <-heartbeatDone })
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			_ = node.WriteMessageAll(heartbeat())
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
		}
	}()
	waitFor(t, func() bool { s.mu.Lock(); defer s.mu.Unlock(); return !s.vehicles[1].HeartbeatAt.IsZero() })
	return r
}
func (r *trustedRig) unlock(t *testing.T) {
	t.Helper()
	status, err := r.s.ConnectTrusted(1)
	if err != nil {
		t.Fatal(err)
	}
	if status.Ready {
		t.Fatal("pilot prerequisite bypassed")
	}
	if err = r.s.SelectTrustedPilot(pilotID); err != nil {
		t.Fatal(err)
	}
}
func TestTrustedHandshakeGatesAndExpiry(t *testing.T) {
	r := newTrustedRig(t)
	if _, err := r.s.GetTrustedPlans(); err == nil {
		t.Fatal("unverified API accepted")
	}
	r.mu.Lock()
	r.pilotActive = false
	r.mu.Unlock()
	if _, err := r.s.ConnectTrusted(1); err != nil {
		t.Fatal(err)
	}
	if err := r.s.SelectTrustedPilot(pilotID); err == nil {
		t.Fatal("inactive pilot accepted")
	}
	r.mu.Lock()
	r.pilotActive = true
	r.mu.Unlock()
	r.unlock(t)
	if !r.s.GetTrustedStatus().Ready {
		t.Fatal("handshake failed")
	}
	r.s.trusted.mu.Lock()
	r.s.trusted.expires = time.Now().Add(-time.Second)
	r.s.trusted.mu.Unlock()
	if r.s.GetTrustedStatus().Ready {
		t.Fatal("expired authentication unlocked")
	}
	if _, err := r.s.GetTrustedPlans(); err == nil {
		t.Fatal("expired token used")
	}
}
func TestTrustedPermissionAndMAVFTP(t *testing.T) {
	r := newTrustedRig(t)
	r.unlock(t)
	for _, status := range []string{"denied", "pending"} {
		r.mu.Lock()
		r.permission = status
		r.mu.Unlock()
		if err := r.s.LoadTrustedOperation(operationID); err == nil || !strings.Contains(err.Error(), status) {
			t.Fatalf("%s: %v", status, err)
		}
		r.mu.Lock()
		writes := r.ftpWrites
		r.mu.Unlock()
		if writes != 0 {
			t.Fatal("transferred without permission")
		}
	}
	r.mu.Lock()
	r.permission = "granted"
	r.drop = true
	r.files["trusted_flight/manifest.json"] = []byte(`{"old":true}`)
	r.mu.Unlock()
	if err := r.s.LoadTrustedOperation(operationID); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, path := range []string{"plan.json", "geocage.json", "permission.json", "manifest.json"} {
		if !json.Valid(r.files["trusted_flight/"+path]) {
			t.Fatalf("missing or corrupt %s", path)
		}
	}
	var manifest map[string]any
	_ = json.Unmarshal(r.files["trusted_flight/manifest.json"], &manifest)
	if manifest["operation_id"] != operationID {
		t.Fatal("wrong transfer manifest")
	}
}
func TestTrustedImportAndSigningRetries(t *testing.T) {
	r := newTrustedRig(t)
	r.unlock(t)
	path := filepath.Join(t.TempDir(), "mission.plan")
	if err := os.WriteFile(path, []byte(`{"fileType":"Plan","mission":{"items":[]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(5 * time.Minute)
	end := start.Add(time.Hour)
	first, err := r.s.ImportTrustedPlan(path, start, end)
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.s.ImportTrustedPlan(path, start, end)
	if err != nil || first != second {
		t.Fatalf("retry: %v %v", second, err)
	}
	r.mu.Lock()
	if r.posts["plans"] != 1 || r.posts["operations"] != 1 {
		t.Fatal("duplicate plan or operation")
	}
	r.failSign = true
	r.mu.Unlock()
	if _, err = r.s.CollectTrustedLog(operationID, "trusted_flight/flight_log.json"); err == nil {
		t.Fatal("server signing failure hidden")
	}
	r.mu.Lock()
	r.failSign = false
	r.mu.Unlock()
	r.unlock(t)
	r.s.trusted.mu.Lock()
	var retryID string
	for _, job := range r.s.trusted.jobs {
		if job.OperationID == operationID && job.LogID != "" {
			retryID = job.LogID
		}
	}
	r.s.trusted.mu.Unlock()
	job, err := r.s.RetryTrustedLog(retryID)
	if err != nil || job.Stage != "signed" {
		t.Fatalf("sign: %v %v", job, err)
	}
	if _, err = r.s.CollectTrustedLog(operationID, "trusted_flight/flight_log.json"); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.posts["logs"] != 1 || r.posts["sign"] != 1 {
		t.Fatal("retry duplicated log or signature")
	}
}
func TestTrustedWatcherStableFiles(t *testing.T) {
	r := newTrustedRig(t)
	r.unlock(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "auto.plan")
	_ = os.WriteFile(path, []byte(`{"fileType":"Plan","mission":{"items":[]}}`), 0600)
	old := time.Now().Add(-time.Minute)
	_ = os.Chtimes(path, old, old)
	r.s.trusted.mu.Lock()
	defer r.s.trusted.mu.Unlock()
	r.s.trusted.config.WatchDirectory = dir
	for range 3 {
		if err := r.s.watchTrustedPlans(context.Background(), r.s.trusted); err != nil {
			t.Fatal(err)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.posts["plans"] != 1 || r.posts["operations"] != 1 {
		t.Fatal("watch failed or duplicated stable file")
	}
}
func TestTrustedConfigSecretAndIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aerobridge_config.json")
	c := TrustedConfig{TimeoutSeconds: 30}
	if err := saveTrustedConfig(path, c); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("config permissions")
	}
	loaded, err := loadTrustedConfig(path)
	if err != nil || loaded.Enabled {
		t.Fatal("disabled config requires credentials")
	}
	c.Enabled = true
	if err = c.validate(); err == nil {
		t.Fatal("invalid enabled config accepted")
	}
}

func TestTrustedPublicKeyAndComponents(t *testing.T) {
	r := newTrustedRig(t)
	r.unlock(t)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.files["trusted_flight/public_key.pem"] = pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})
	r.mu.Unlock()
	for range 2 {
		if err = r.s.RegisterTrustedPublicKey("trusted_flight/public_key.pem"); err != nil {
			t.Fatal(err)
		}
	}
	r.mu.Lock()
	if r.posts["credentials"] != 1 {
		t.Fatal("credential retry duplicate")
	}
	r.files["trusted_flight/private_key.pem"] = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})
	r.mu.Unlock()
	if err = r.s.RegisterTrustedPublicKey("trusted_flight/private_key.pem"); err == nil {
		t.Fatal("private key accepted")
	}
	if err = r.s.VerifyTrustedComponent("component-1"); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.credentials = append(r.credentials, map[string]any{"id": newID(), "aircraft": aircraftID, "token_type": "x509 Digital Certificate", "is_active": true, "token": "certificate"}, map[string]any{"id": newID(), "aircraft": newID(), "token_type": "x509 Digital Certificate", "is_active": true})
	r.mu.Unlock()
	certs, err := r.s.GetTrustedCertificates()
	if err != nil || len(certs) != 1 {
		t.Fatalf("certificate filtering: %v %v", certs, err)
	}
}
func TestTrustedTransferIntegrityFailure(t *testing.T) {
	r := newTrustedRig(t)
	r.unlock(t)
	r.mu.Lock()
	r.corrupt = true
	r.files["trusted_flight/manifest.json"] = []byte(`{"old":true}`)
	r.mu.Unlock()
	if err := r.s.LoadTrustedOperation(operationID); err == nil || !strings.Contains(err.Error(), "read-back") {
		t.Fatalf("corrupt transfer accepted: %v", err)
	}
	r.mu.Lock()
	_, complete := r.files["trusted_flight/manifest.json"]
	r.mu.Unlock()
	if complete {
		t.Fatal("failed transfer left completion marker")
	}
}
func TestTrustedIdentityLossRelocks(t *testing.T) {
	r := newTrustedRig(t)
	r.unlock(t)
	r.s.mu.Lock()
	v := r.s.vehicles[1]
	v.ControllerID = "different"
	r.s.vehicles[1] = v
	r.s.mu.Unlock()
	if r.s.GetTrustedStatus().Ready {
		t.Fatal("identity change kept unlocked")
	}
	if err := r.s.SelectTrustedPilot(pilotID); err == nil {
		t.Fatal("pilot selection bypassed fresh aircraft verification")
	}
	r.unlock(t)
	config := r.s.GetTrustedConfig()
	if config.FlightPassport.ClientSecret != "" {
		t.Fatal("UI getter exposed secret")
	}
	config.Enabled = false
	if err := r.s.ApplyTrustedConfig(config); err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.GetTrustedPlans(); err == nil {
		t.Fatal("disabled module accepted command")
	}
	loaded, err := loadTrustedConfig(trustedConfigPath(r.s.path))
	if err != nil || loaded.Enabled || loaded.FlightPassport.ClientSecret != "secret" {
		t.Fatalf("config round-trip: %v", err)
	}
}

func TestTrustedLostPlanResponse(t *testing.T) {
	r := newTrustedRig(t)
	r.unlock(t)
	path := filepath.Join(t.TempDir(), "mission.plan")
	if err := os.WriteFile(path, []byte(`{"fileType":"Plan","mission":{"items":[]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(5 * time.Minute)
	r.mu.Lock()
	r.losePlanResponse = true
	r.mu.Unlock()
	if _, err := r.s.ImportTrustedPlan(path, start, start.Add(time.Hour)); err == nil {
		t.Fatal("expected lost response")
	}
	renamed := filepath.Join(filepath.Dir(path), "renamed.plan")
	if err := os.Rename(path, renamed); err != nil {
		t.Fatal(err)
	}
	r.unlock(t)
	if _, err := r.s.ImportTrustedPlan(renamed, start, start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.posts["plans"] != 1 || r.posts["operations"] != 1 {
		t.Fatal("retry duplicated records")
	}
}
func TestTrustedLogJSONPrecision(t *testing.T) {
	if sameTrustedJSON([]byte(`{"value":9007199254740992}`), []byte(`{"value":9007199254740993}`)) {
		t.Fatal("distinct log samples treated as identical")
	}
	if !sameTrustedJSON([]byte(`{"a":1,"b":2}`), []byte(`{ "b": 2, "a": 1 }`)) {
		t.Fatal("object ordering affected reconciliation")
	}
}

func TestTrustedMalformedReceipt(t *testing.T) {
	for _, signed := range []string{`null`, `""`, `{}`, `{"signature":""}`, `[]`} {
		raw := []byte(`{"raw_flight_log":"` + operationID + `","signed_log":` + signed + `}`)
		if validTrustedReceipt(raw, operationID) {
			t.Fatalf("accepted missing signature: %s", signed)
		}
	}
	for _, signed := range []string{`"signed-token"`, `{"signature":"signed-token"}`} {
		raw := []byte(`{"raw_flight_log":"` + operationID + `","signed_log":` + signed + `}`)
		if !validTrustedReceipt(raw, operationID) {
			t.Fatalf("rejected receipt: %s", signed)
		}
		if validTrustedReceipt(raw, planID) {
			t.Fatal("accepted signature for another log")
		}
	}
}
func TestTrustedWatcherSkipsInvalidPlan(t *testing.T) {
	r := newTrustedRig(t)
	r.unlock(t)
	dir := t.TempDir()
	for name, raw := range map[string]string{"a-invalid.plan": "{", "z-valid.plan": `{"fileType":"Plan","mission":{"items":[]}}`} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-time.Minute)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	r.s.op.Lock()
	defer r.s.op.Unlock()
	tm := r.s.trusted
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.config.WatchDirectory = dir
	if err := r.s.watchTrustedPlans(context.Background(), tm); err != nil {
		t.Fatal(err)
	}
	if err := r.s.watchTrustedPlans(context.Background(), tm); err == nil {
		t.Fatal("invalid plan error hidden")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.posts["plans"] != 1 || r.posts["operations"] != 1 {
		t.Fatal("invalid plan prevented valid import")
	}
}

func TestTrustedFirmwareDERKey(t *testing.T) {
	r := newTrustedRig(t)
	r.unlock(t)
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.files["APM/self_pubkey.der"] = x509.MarshalPKCS1PublicKey(&private.PublicKey)
	r.files["APM/private.der"] = x509.MarshalPKCS1PrivateKey(private)
	r.mu.Unlock()
	if err := r.s.RegisterTrustedPublicKey("APM/self_pubkey.der"); err != nil {
		t.Fatal(err)
	}
	if err := r.s.RegisterTrustedPublicKey("APM/private.der"); err == nil {
		t.Fatal("DER private key accepted")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.credentials) != 1 {
		t.Fatal("unexpected credential count")
	}
	block, rest := pem.Decode([]byte(r.credentials[0]["token"].(string)))
	if block == nil || block.Type != "PUBLIC KEY" || len(rest) != 0 {
		t.Fatal("did not send canonical public PEM")
	}
	public, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil || !bytes.Equal(x509.MarshalPKCS1PublicKey(public.(*rsa.PublicKey)), x509.MarshalPKCS1PublicKey(&private.PublicKey)) {
		t.Fatal("public key changed")
	}
}
func TestTrustedVehiclePaths(t *testing.T) {
	for _, path := range []string{"", "/absolute", "../key", "APM/../key", "APM//key", "APM/./key", "APM/key\x00", `APM\key`} {
		if validTrustedVehiclePath(path) {
			t.Fatalf("accepted invalid path %q", path)
		}
	}
	for _, path := range []string{"APM/self_pubkey.der", "trusted_flight/flight_log.json", "APM/npntlog_2026-11-04.json"} {
		if !validTrustedVehiclePath(path) {
			t.Fatalf("rejected firmware path %q", path)
		}
	}
}
