package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type TrustedStatus struct {
	Enabled      bool             `json:"enabled"`
	Ready        bool             `json:"ready"`
	Drone        Link             `json:"drone"`
	Server       Link             `json:"server"`
	SystemID     int              `json:"systemID"`
	ControllerID string           `json:"controllerID"`
	AircraftID   string           `json:"aircraftID"`
	PilotID      string           `json:"pilotID"`
	Pilots       []RegistryEntity `json:"pilots"`
	Detail       string           `json:"detail"`
}
type trustedModule struct {
	loadErr error
	mu      sync.Mutex
	config  TrustedConfig
	status  TrustedStatus
	api     *Aerobridge
	expires time.Time
	// Reconciliation markers are persisted before remote writes; PRO assigns record UUIDs.
	watchSeen map[string]time.Time
	jobs      map[string]TrustedJob
}
type TrustedJob struct {
	PlanID      string `json:"planID"`
	OperationID string `json:"operationID"`
	LogID       string `json:"logID"`
	Stage       string `json:"stage"`
	Marker      string `json:"marker"`
}

func (s *Service) GetTrustedConfig() TrustedConfig {
	s.trusted.mu.Lock()
	defer s.trusted.mu.Unlock()
	c := s.trusted.config
	c.FlightPassport.ClientSecret = ""
	return c
}
func (s *Service) ApplyTrustedConfig(c TrustedConfig) error {
	s.op.Lock()
	defer s.op.Unlock()
	t := s.trusted
	t.mu.Lock()
	defer t.mu.Unlock()
	if c.Enabled && t.loadErr != nil {
		return fmt.Errorf("restore aerobridge_jobs.json and restart before enabling Trusted Flight: %w", t.loadErr)
	}
	if c.FlightPassport.ClientSecret == "" {
		c.FlightPassport.ClientSecret = t.config.FlightPassport.ClientSecret
	}
	if err := saveTrustedConfig(trustedConfigPath(s.path), c); err != nil {
		return err
	}
	t.config = c
	t.api = nil
	t.status = TrustedStatus{Enabled: c.Enabled, Detail: "Complete the connection handshake"}
	return nil
}
func (s *Service) GetTrustedStatus() TrustedStatus {
	t := s.trusted
	t.mu.Lock()
	defer t.mu.Unlock()
	s.trustedFresh(t)
	out := t.status
	out.Pilots = append([]RegistryEntity{}, out.Pilots...)
	return out
}
func (s *Service) trustedFresh(t *trustedModule) bool {
	s.mu.Lock()
	v, ok := s.vehicles[byte(t.status.SystemID)]
	running := s.state.Running
	s.mu.Unlock()
	fresh := t.config.Enabled && running && ok && time.Since(v.HeartbeatAt) < 5*time.Second && v.ControllerID == t.status.ControllerID && time.Now().Before(t.expires)
	if t.api != nil && !time.Now().Before(t.expires) {
		t.status.Server = Link{State: "waiting", Detail: "Passport authentication required"}
	}
	if !fresh {
		t.status.Ready = false
		t.status.AircraftID = ""
		t.status.PilotID = ""
		t.status.Drone = Link{State: "waiting", Detail: "Connect the verified vehicle and repeat handshake"}
	}
	return fresh
}
func (t *trustedModule) authenticate(ctx context.Context) error {
	c := t.config
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {c.FlightPassport.ClientID}, "client_secret": {c.FlightPassport.ClientSecret}, "audience": {c.FlightPassport.Audience}, "scope": {"aerobridge.read aerobridge.write"}}
	endpoint := strings.TrimRight(c.FlightPassport.URL, "/") + "/" + strings.TrimLeft(c.FlightPassport.TokenURL, "/")
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: time.Duration(c.TimeoutSeconds) * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Passport is unreachable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("Passport returned HTTP %d", res.StatusCode)
	}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&token) != nil || token.AccessToken == "" || token.ExpiresIn <= 0 {
		return fmt.Errorf("invalid Passport token response")
	}
	t.api = NewAerobridge(Config{AerobridgeURL: c.ServerURL, AerobridgeToken: token.AccessToken, AerobridgeEnabled: true})
	t.api.client.client = client
	t.expires = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	return nil
}

// ConnectTrusted authenticates before querying the vehicle registry. Pilot selection
// is a separate step so no fleet command can run with an implicit assignment.
func (s *Service) ConnectTrusted(systemID int) (result TrustedStatus, err error) {
	s.op.Lock()
	defer s.op.Unlock()
	t := s.trusted
	t.mu.Lock()
	defer t.mu.Unlock()
	return s.connectTrusted(context.Background(), t, systemID)
}
func (s *Service) connectTrusted(parent context.Context, t *trustedModule, systemID int) (result TrustedStatus, err error) {
	t.status = TrustedStatus{Enabled: t.config.Enabled, SystemID: systemID, Detail: "Handshake incomplete"}
	defer func() {
		if err != nil {
			t.status.Ready = false
			t.status.Detail = err.Error()
			t.status.Server = linkResult(err, "")
		}
		result = t.status
	}()
	if !t.config.Enabled {
		return result, fmt.Errorf("enable Trusted Flight first")
	}
	if systemID < 1 || systemID > 255 {
		return result, fmt.Errorf("select one vehicle system ID")
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(t.config.TimeoutSeconds)*time.Second)
	defer cancel()
	if err = t.authenticate(ctx); err != nil {
		return
	}
	if _, err = t.api.request(ctx, "GET", "/ping/", nil, false); err != nil {
		return
	}
	// A direct or bidirectional router connection is necessary for this request.
	if err = s.requestController(ctx, byte(systemID)); err != nil {
		return
	}
	s.mu.Lock()
	v := s.vehicles[byte(systemID)]
	s.mu.Unlock()
	if time.Since(v.HeartbeatAt) > 5*time.Second || v.ControllerID == "" {
		return result, fmt.Errorf("waiting for fresh vehicle heartbeat and AUTOPILOT_VERSION UID2")
	}
	t.status.ControllerID = v.ControllerID
	t.status.Drone = linkResult(nil, "Flight controller identified")
	var aircraft struct {
		ID         string `json:"id"`
		Status     int    `json:"status"`
		Controller string `json:"flight_controller_id"`
	}
	raw, e := t.api.request(ctx, "GET", "/registry/aircraft/rfm/"+url.PathEscape(v.ControllerID), nil, true)
	if e != nil {
		err = e
		return
	}
	if json.Unmarshal(raw, &aircraft) != nil || !validUUID(aircraft.ID) || aircraft.Status != 1 || aircraft.Controller != v.ControllerID {
		return result, fmt.Errorf("aircraft identity is unregistered, mismatched or inactive")
	}
	t.status.AircraftID = aircraft.ID
	t.status.Pilots, err = t.api.list(ctx, "/registry/pilots/")
	if err != nil {
		return
	}
	t.status.Server = linkResult(nil, "Passport and aircraft verification succeeded")
	t.status.Detail = "Select an active pilot to unlock Fleet Management"
	return
}
func (s *Service) SelectTrustedPilot(id string) error {
	s.op.Lock()
	defer s.op.Unlock()
	t := s.trusted
	t.mu.Lock()
	defer t.mu.Unlock()
	return s.selectTrustedPilot(t, id)
}
func (s *Service) selectTrustedPilot(t *trustedModule, id string) error {
	t.status.Ready = false
	t.status.PilotID = ""
	if !s.trustedFresh(t) || t.status.AircraftID == "" || t.api == nil {
		return fmt.Errorf("repeat the vehicle/server handshake")
	}
	for _, p := range t.status.Pilots {
		if p.ID == id {
			var pilot struct {
				Active   bool   `json:"is_active"`
				Operator string `json:"operator"`
			}
			if json.Unmarshal(p.Data, &pilot) != nil || !pilot.Active || pilot.Operator != t.config.Credentials.OperatorID {
				return fmt.Errorf("pilot must be active and assigned to this operator")
			}
			t.status.PilotID = id
			t.status.Ready = true
			t.status.Detail = "Trusted Flight is ready"
			return nil
		}
	}
	return fmt.Errorf("select a pilot from the verified registry")
}
func (s *Service) trustedCommand(fn func(context.Context, *trustedModule) error) error {
	s.op.Lock()
	defer s.op.Unlock()
	t := s.trusted
	t.mu.Lock()
	defer t.mu.Unlock()
	if !s.trustedFresh(t) || !t.status.Ready || t.api == nil {
		return fmt.Errorf("complete the Trusted Flight handshake first")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(t.config.TimeoutSeconds)*time.Second)
	defer cancel()
	err := fn(ctx, t)
	if err != nil {
		trustedFailure(t, err)
	} else {
		t.status.Detail = "Command completed"
	}
	return err
}
func (s *Service) GetTrustedPlans() (out []RegistryEntity, err error) {
	err = s.trustedCommand(func(ctx context.Context, t *trustedModule) error {
		var e error
		out, e = t.api.list(ctx, "/gcs/flight-plans")
		return e
	})
	return
}
func (s *Service) GetTrustedOperations() (out []RegistryEntity, err error) {
	err = s.trustedCommand(func(ctx context.Context, t *trustedModule) error {
		var e error
		out, e = t.api.list(ctx, "/gcs/flight-operations")
		return e
	})
	return
}
func (s *Service) trustedJobsPath() string {
	return filepath.Join(filepath.Dir(s.path), "aerobridge_jobs.json")
}
func (s *Service) persistTrustedJobs(t *trustedModule) error {
	raw, err := json.Marshal(t.jobs)
	if err != nil {
		return err
	}
	return atomicTrustedFile(s.trustedJobsPath(), raw)
}
func (s *Service) CreateTrustedOperation(planID, name string, start, end time.Time) (out TrustedJob, err error) {
	err = s.trustedCommand(func(ctx context.Context, t *trustedModule) error {
		var e error
		out, e = s.createTrustedOperation(ctx, t, planID, name, start, end)
		return e
	})
	return
}
func (s *Service) createTrustedOperation(ctx context.Context, t *trustedModule, planID, name string, start, end time.Time) (TrustedJob, error) {
	if !validUUID(planID) || !end.After(start) {
		return TrustedJob{}, fmt.Errorf("select a plan and a future start/end time")
	}
	key := trustedScope(t) + ":" + planID + ":" + t.status.AircraftID + ":" + t.status.PilotID + ":" + start.UTC().Format(time.RFC3339Nano)
	job := t.jobs[key]
	if job.OperationID == "" {
		job = TrustedJob{PlanID: planID, OperationID: newID(), Marker: newID()}
		t.jobs[key] = job
		if e := s.persistTrustedJobs(t); e != nil {
			return job, e
		}
	}
	path := "/gcs/flight-operations"
	// Reconcile server-assigned IDs by a persisted unique name after a lost response.
	remoteName := trustedName(name, job.Marker)
	existing, e := t.api.list(ctx, path)
	if e != nil {
		return job, e
	}
	for _, item := range existing {
		if item.ID == job.OperationID || trustedMarker(item, job.Marker) {
			job.OperationID = item.ID
			t.jobs[key] = job
			return job, s.persistTrustedJobs(t)
		}
	}
	if !start.After(time.Now()) {
		return job, fmt.Errorf("operation start time has passed; choose a future interval")
	}
	response, e := t.api.request(ctx, "POST", path, map[string]any{"name": remoteName, "organization": t.config.Credentials.OrganizationID, "operator": t.config.Credentials.OperatorID, "drone": t.status.AircraftID, "pilot": t.status.PilotID, "flight_plan": planID, "start_datetime": start, "end_datetime": end}, true)
	if e != nil {
		return job, e
	}
	id, e := trustedResponseID(response)
	if e != nil {
		return job, e
	}
	job.OperationID = id
	t.jobs[key] = job
	return job, s.persistTrustedJobs(t)
}
func (s *Service) ImportTrustedPlan(path string, start, end time.Time) (out TrustedJob, err error) {
	err = s.trustedCommand(func(ctx context.Context, t *trustedModule) error {
		var e error
		out, e = s.importTrustedPlan(ctx, t, path, start, end)
		return e
	})
	return
}
func (s *Service) importTrustedPlan(ctx context.Context, t *trustedModule, path string, start, end time.Time) (TrustedJob, error) {
	info, e := os.Lstat(path)
	if e != nil {
		return TrustedJob{}, e
	}
	if !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return TrustedJob{}, fmt.Errorf("plan must be a regular file of at most 8 MiB")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return TrustedJob{}, e
	}
	if !validTrustedPlan(raw) {
		return TrustedJob{}, fmt.Errorf("invalid QGroundControl Plan JSON")
	}
	hash := sha256.Sum256(raw)
	key := trustedScope(t) + ":" + t.status.AircraftID + ":" + t.status.PilotID + ":" + hex.EncodeToString(hash[:])
	job := t.jobs[key]
	if job.PlanID == "" {
		job.PlanID = newID()
		job.Marker = newID()
		t.jobs[key] = job
		if e = s.persistTrustedJobs(t); e != nil {
			return job, e
		}
	}
	plans, e := t.api.list(ctx, "/gcs/flight-plans")
	if e != nil {
		return job, e
	}
	remoteName := trustedName(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), job.Marker)
	found := false
	for _, p := range plans {
		if p.ID == job.PlanID || trustedMarker(p, job.Marker) {
			found = true
			job.PlanID = p.ID
			t.jobs[key] = job
			if e = s.persistTrustedJobs(t); e != nil {
				return job, e
			}
		}
	}
	if !found {
		var response []byte
		response, e = t.api.request(ctx, "POST", "/gcs/flight-plans", map[string]any{"name": remoteName, "organization": t.config.Credentials.OrganizationID, "plan_file_json": json.RawMessage(raw)}, true)
		if e != nil {
			return job, e
		}
		job.PlanID, e = trustedResponseID(response)
		if e != nil {
			return job, e
		}
		t.jobs[key] = job
		if e = s.persistTrustedJobs(t); e != nil {
			return job, e
		}
		found = true
	}
	if !found {
		return job, fmt.Errorf("plan persistence failed")
	}
	if job.OperationID == "" {
		marker := job.Marker
		job, e = s.createTrustedOperation(ctx, t, job.PlanID, filepath.Base(path), start, end)
		if e != nil {
			return job, e
		}
		job.Marker = marker
		t.jobs[key] = job
		if e = s.persistTrustedJobs(t); e != nil {
			return job, e
		}
	}
	return job, nil
}

func trustedName(name, marker string) string {
	r := []rune(name)
	if len(r) > 90 {
		r = r[:90]
	}
	return string(r) + " [adapter:" + marker + "]"
}
func entityName(item RegistryEntity) string {
	var v struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(item.Data, &v)
	return v.Name
}
func trustedResponseID(raw []byte) (string, error) {
	var v struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &v) != nil || !validUUID(v.ID) {
		return "", fmt.Errorf("server did not return a valid record UUID")
	}
	return v.ID, nil
}

// GetTrustedJobs exposes durable log receipts for retry after server signing fails.
func (s *Service) GetTrustedJobs() (out []TrustedJob, err error) {
	err = s.trustedCommand(func(_ context.Context, t *trustedModule) error {
		out = []TrustedJob{}
		seen := map[string]bool{}
		for key, job := range t.jobs {
			if strings.HasPrefix(key, trustedScope(t)+":") && job.LogID != "" && job.OperationID != "" && !seen[job.LogID] {
				out = append(out, job)
				seen[job.LogID] = true
			}
		}
		return nil
	})
	return
}

func trustedScope(t *trustedModule) string {
	return t.config.ServerURL + ":" + t.config.Credentials.OrganizationID + ":" + t.config.Credentials.OperatorID
}
func trustedMarker(item RegistryEntity, marker string) bool {
	return marker != "" && strings.HasSuffix(entityName(item), " [adapter:"+marker+"]")
}

func validTrustedPlan(raw []byte) bool {
	var plan struct {
		FileType string `json:"fileType"`
		Mission  struct {
			Items []json.RawMessage `json:"items"`
		} `json:"mission"`
	}
	return json.Unmarshal(raw, &plan) == nil && plan.FileType == "Plan" && plan.Mission.Items != nil
}

func trustedFailure(t *trustedModule, err error) {
	var remote *aerobridgeError
	if errors.As(err, &remote) {
		t.status.Ready = false
		t.status.AircraftID = ""
		t.status.PilotID = ""
		t.status.Server = linkResult(err, "")
	}
	t.status.Detail = err.Error()
}
