package adapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type trustedOperation struct {
	ID           string `json:"id"`
	Aircraft     string `json:"drone"`
	Pilot        string `json:"pilot"`
	Plan         string `json:"flight_plan"`
	Organization string `json:"organization"`
	Operator     string `json:"operator"`
}

func (t *trustedModule) operation(ctx context.Context, id string) (trustedOperation, error) {
	var op trustedOperation
	if !validUUID(id) {
		return op, fmt.Errorf("invalid operation UUID")
	}
	raw, err := t.api.request(ctx, "GET", "/gcs/flight-operations/"+id, nil, true)
	if err != nil {
		return op, err
	}
	if json.Unmarshal(raw, &op) != nil || op.ID != id || !validUUID(op.Plan) || op.Aircraft != t.status.AircraftID || op.Pilot != t.status.PilotID || op.Organization != t.config.Credentials.OrganizationID || op.Operator != t.config.Credentials.OperatorID {
		return op, fmt.Errorf("operation must match the verified aircraft, pilot, operator and organization")
	}
	return op, nil
}

// LoadTrustedOperation transfers the exact server plan and granted artifacts.
// It never arms the aircraft. Completion is reported only after read-back checks.
func (s *Service) LoadTrustedOperation(id string) error {
	return s.trustedCommand(func(ctx context.Context, t *trustedModule) error {
		op, err := t.operation(ctx, id)
		if err != nil {
			return err
		}
		s.mu.Lock()
		armed := s.vehicles[byte(t.status.SystemID)].Armed
		s.mu.Unlock()
		if armed {
			return fmt.Errorf("disarm the vehicle before loading artifacts")
		}
		raw, err := t.api.request(ctx, "PUT", "/gcs/flight-operations/"+id+"/permission", nil, true)
		if err != nil {
			return err
		}
		var permission struct {
			Status string          `json:"status_code"`
			Token  json.RawMessage `json:"token"`
			Cage   []struct {
				Lat float64 `json:"lat"`
				Lng float64 `json:"lng"`
			} `json:"geo_cage"`
			Operation trustedOperation `json:"operation"`
		}
		if json.Unmarshal(raw, &permission) != nil || permission.Operation.ID != id || permission.Operation.Plan != op.Plan {
			return fmt.Errorf("permission does not match the selected operation and plan")
		}
		if permission.Status != "granted" {
			return fmt.Errorf("flight permission is %s", permission.Status)
		}
		var token struct {
			AccessToken string `json:"access_token"`
		}
		if json.Unmarshal(permission.Token, &token) != nil || token.AccessToken == "" {
			return fmt.Errorf("granted permission has no token")
		}
		if len(permission.Cage) < 3 {
			return fmt.Errorf("permission has no geo-cage polygon")
		}
		for _, p := range permission.Cage {
			if p.Lat < -90 || p.Lat > 90 || p.Lng < -180 || p.Lng > 180 {
				return fmt.Errorf("invalid geo-cage coordinate")
			}
		}
		rawPlan, err := t.api.request(ctx, "GET", "/gcs/flight-plans/"+op.Plan, nil, true)
		if err != nil {
			return err
		}
		var plan struct {
			ID   string          `json:"id"`
			Data json.RawMessage `json:"plan_file_json"`
		}
		if json.Unmarshal(rawPlan, &plan) != nil || plan.ID != op.Plan || !validTrustedPlan(plan.Data) {
			return fmt.Errorf("invalid stored flight plan")
		}
		system := byte(t.status.SystemID)
		if err = s.trustedDirectory(ctx, system); err != nil {
			return err
		}
		// Invalidate any previous completion marker before replacing artifacts.
		_, err = s.ftp(ctx, system, 0, 8, 0, []byte("trusted_flight/manifest.json\x00"), byte(len("trusted_flight/manifest.json")+1))
		if err != nil && err != ftpError(10) {
			return err
		}
		cage, _ := json.Marshal(permission.Cage)
		files := []struct {
			name string
			data []byte
		}{{"plan.json", plan.Data}, {"geocage.json", cage}, {"permission.json", permission.Token}}
		// The manifest is written last, so a failed transfer never claims completion.
		manifest := map[string]any{"operation_id": id, "flight_plan_id": op.Plan, "sha256": map[string]string{}}
		hashes := manifest["sha256"].(map[string]string)
		for _, f := range files {
			if !s.trustedFresh(t) {
				return fmt.Errorf("vehicle disconnected during transfer")
			}
			path := "trusted_flight/" + f.name
			if err = s.ftpUpload(ctx, system, path, f.data); err != nil {
				return err
			}
			readback, e := s.ftpDownload(ctx, system, path)
			if e != nil {
				return e
			}
			expected := sha256.Sum256(f.data)
			actual := sha256.Sum256(readback)
			if expected != actual {
				return fmt.Errorf("vehicle artifact read-back mismatch")
			}
			hashes[f.name] = hex.EncodeToString(expected[:])
		}
		manifestRaw, _ := json.Marshal(manifest)
		if err = s.ftpUpload(ctx, system, "trusted_flight/manifest.json", manifestRaw); err != nil {
			return err
		}
		checked, err := s.ftpDownload(ctx, system, "trusted_flight/manifest.json")
		if err != nil {
			return err
		}
		if sha256.Sum256(checked) != sha256.Sum256(manifestRaw) {
			return fmt.Errorf("manifest read-back mismatch")
		}
		return nil
	})
}

// CollectTrustedLog reads the firmware-produced JSON file through MAVFTP. It
// reconciles stable UUIDs before POST and signing, including lost responses.
func (s *Service) CollectTrustedLog(operationID, remotePath string) (result TrustedJob, err error) {
	err = s.trustedCommand(func(ctx context.Context, t *trustedModule) error {
		if _, e := t.operation(ctx, operationID); e != nil {
			return e
		}
		s.mu.Lock()
		armed := s.vehicles[byte(t.status.SystemID)].Armed
		s.mu.Unlock()
		if armed {
			return fmt.Errorf("disarm the vehicle before collecting its completed flight log")
		}
		if !validTrustedVehiclePath(remotePath) {
			return fmt.Errorf("select a relative vehicle log path without traversal")
		}
		raw, e := s.ftpDownload(ctx, byte(t.status.SystemID), remotePath)
		if e != nil {
			return e
		}
		var log map[string]json.RawMessage
		if json.Unmarshal(raw, &log) != nil || log == nil {
			return fmt.Errorf("PRO raw flight logs must be JSON objects")
		}
		hash := sha256.Sum256(raw)
		key := trustedScope(t) + ":" + operationID + ":" + hex.EncodeToString(hash[:])
		job := t.jobs[key]
		if job.LogID == "" {
			job = TrustedJob{OperationID: operationID, LogID: newID()}
			t.jobs[key] = job
			if e = s.persistTrustedJobs(t); e != nil {
				return e
			}
		}
		result = job
		logs, e := t.api.list(ctx, "/gcs/flight-logs")
		if e != nil {
			return e
		}
		exists := false
		for _, item := range logs {
			var existing struct {
				Operation string          `json:"operation"`
				Raw       json.RawMessage `json:"raw_log"`
			}
			_ = json.Unmarshal(item.Data, &existing)
			if item.ID == job.LogID || (existing.Operation == operationID && sameTrustedJSON(existing.Raw, raw)) {
				exists = true
				job.LogID = item.ID
			}
		}
		if !exists {
			var response []byte
			response, e = t.api.request(ctx, "POST", "/gcs/flight-logs", map[string]any{"operation": operationID, "organization": t.config.Credentials.OrganizationID, "raw_log": json.RawMessage(raw)}, true)
			if e != nil {
				return e
			}
			job.LogID, e = trustedResponseID(response)
			if e != nil {
				return e
			}
		}
		t.jobs[key] = job
		result = job
		if e = atomicTrustedFile(filepath.Join(filepath.Dir(s.path), "aerobridge_logs", job.LogID+".json"), raw); e != nil {
			return e
		}
		if e = s.persistTrustedJobs(t); e != nil {
			return e
		}
		return s.signTrustedLog(ctx, t, key, &result)
	})
	return
}
func (s *Service) signTrustedLog(ctx context.Context, t *trustedModule, key string, result *TrustedJob) error {
	job := t.jobs[key]
	signed, e := t.api.list(ctx, "/gcs/signed-flight-logs")
	if e != nil {
		return e
	}
	for _, item := range signed {
		if validTrustedReceipt(item.Data, job.LogID) {
			job.Stage = "signed"
		}
	}
	if job.Stage != "signed" {
		response, e := t.api.request(ctx, "PUT", "/gcs/flight-logs/"+job.LogID+"/sign", nil, true)
		if e != nil {
			return e
		}
		if !validTrustedReceipt(response, job.LogID) {
			return fmt.Errorf("invalid signed flight log receipt")
		}
		job.Stage = "signed"
	}
	t.jobs[key] = job
	*result = job
	return s.persistTrustedJobs(t)
}

// RetryTrustedLog signs an already uploaded log without fetching it from the vehicle again.
func (s *Service) RetryTrustedLog(id string) (result TrustedJob, err error) {
	err = s.trustedCommand(func(ctx context.Context, t *trustedModule) error {
		if !validUUID(id) {
			return fmt.Errorf("invalid log UUID")
		}
		for key, job := range t.jobs {
			if job.LogID == id && job.OperationID != "" && strings.HasPrefix(key, trustedScope(t)+":") {
				if _, e := t.operation(ctx, job.OperationID); e != nil {
					return e
				}
				if _, e := os.Stat(filepath.Join(filepath.Dir(s.path), "aerobridge_logs", id+".json")); e != nil {
					return e
				}
				return s.signTrustedLog(ctx, t, key, &result)
			}
		}
		return fmt.Errorf("no uploaded flight log with this UUID")
	})
	return
}
func sameTrustedJSON(a, b []byte) bool {
	var x, y any
	da, db := json.NewDecoder(bytes.NewReader(a)), json.NewDecoder(bytes.NewReader(b))
	da.UseNumber()
	db.UseNumber()
	if !json.Valid(a) || !json.Valid(b) || da.Decode(&x) != nil || db.Decode(&y) != nil {
		return false
	}
	xx, _ := json.Marshal(x)
	yy, _ := json.Marshal(y)
	return bytes.Equal(xx, yy)
}
func (s *Service) VerifyTrustedComponent(id string) error {
	return s.trustedCommand(func(ctx context.Context, t *trustedModule) error {
		if id == "" || len(id) > 256 {
			return fmt.Errorf("component ID is required")
		}
		raw, err := t.api.request(ctx, "GET", "/registry/aircraft-component/verify/"+url.PathEscape(id), nil, true)
		if err != nil {
			return err
		}
		var response struct {
			ID     string `json:"aerobridge_id"`
			Status bool   `json:"status"`
		}
		if json.Unmarshal(raw, &response) != nil || response.ID != id || !response.Status {
			return fmt.Errorf("component authenticity verification failed")
		}
		return nil
	})
}

// RegisterTrustedPublicKey never handles a private key. Generation must happen
// on the aircraft using its firmware-specific command, outside standard MAVFTP.
func (s *Service) RegisterTrustedPublicKey(remotePath string) error {
	return s.trustedCommand(func(ctx context.Context, t *trustedModule) error {
		if !validTrustedVehiclePath(remotePath) {
			return fmt.Errorf("select a relative vehicle public-key path without traversal")
		}
		raw, err := s.ftpDownload(ctx, byte(t.status.SystemID), remotePath)
		if err != nil {
			return err
		}
		raw, err = trustedPublicKeyPEM(raw)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(raw)
		key := trustedScope(t) + ":" + t.status.AircraftID + ":" + hex.EncodeToString(digest[:])
		job := t.jobs[key]
		if job.Stage == "registered" {
			return nil
		}
		if job.LogID == "" {
			job.LogID = newID()
			t.jobs[key] = job
			if err = s.persistTrustedJobs(t); err != nil {
				return err
			}
		}
		credentials, err := t.api.list(ctx, "/pki/credentials/")
		if err != nil {
			return err
		}
		for _, item := range credentials {
			if item.ID == job.LogID || entityName(item) == trustedName("Vehicle public key", job.LogID) {
				job.Stage = "registered"
				t.jobs[key] = job
				return s.persistTrustedJobs(t)
			}
		}
		_, err = t.api.request(ctx, "POST", "/pki/credentials/", map[string]any{"name": trustedName("Vehicle public key", job.LogID), "token": string(raw), "token_type": 0, "extension": 0, "association": 3, "aircraft": t.status.AircraftID, "organization": t.config.Credentials.OrganizationID, "manufacturer": nil, "operator": nil, "is_active": true}, true)
		if err == nil {
			job.Stage = "registered"
			t.jobs[key] = job
			err = s.persistTrustedJobs(t)
		}
		return err
	})
}
func (s *Service) GetTrustedCertificates() (out []RegistryEntity, err error) {
	err = s.trustedCommand(func(ctx context.Context, t *trustedModule) error {
		items, e := t.api.list(ctx, "/pki/credentials/")
		if e != nil {
			return e
		}
		out = []RegistryEntity{}
		for _, item := range items {
			var c struct {
				Aircraft string `json:"aircraft"`
				Type     string `json:"token_type"`
				Active   bool   `json:"is_active"`
			}
			if json.Unmarshal(item.Data, &c) == nil && c.Aircraft == t.status.AircraftID && c.Type == "x509 Digital Certificate" && c.Active {
				out = append(out, item)
			}
		}
		return nil
	})
	return
}

func validTrustedReceipt(raw []byte, logID string) bool {
	var receipt struct {
		Raw    string          `json:"raw_flight_log"`
		Signed json.RawMessage `json:"signed_log"`
	}
	if json.Unmarshal(raw, &receipt) != nil || receipt.Raw != logID {
		return false
	}
	var token string
	if json.Unmarshal(receipt.Signed, &token) == nil {
		return strings.TrimSpace(token) != ""
	}
	var signed map[string]json.RawMessage
	if json.Unmarshal(receipt.Signed, &signed) != nil {
		return false
	}
	return json.Unmarshal(signed["signature"], &token) == nil && strings.TrimSpace(token) != ""
}

// Firmware may expose a DER key (e.g. CubePilot self_pubkey.der) or PEM.
// Decode only public-key structures; never forward the input bytes unchecked.
func trustedPublicKeyPEM(raw []byte) ([]byte, error) {
	der := raw
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("-----BEGIN")) {
		block, rest := pem.Decode(raw)
		if block == nil || (block.Type != "PUBLIC KEY" && block.Type != "RSA PUBLIC KEY") || len(bytes.TrimSpace(rest)) != 0 {
			return nil, fmt.Errorf("vehicle must supply one public key; private keys are never uploaded")
		}
		der = block.Bytes
	}
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		key, err = x509.ParsePKCS1PublicKey(der)
	}
	if err != nil {
		return nil, fmt.Errorf("vehicle must supply a PEM or DER public key; private keys are never uploaded")
	}
	canonical, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return nil, fmt.Errorf("invalid vehicle public key")
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: canonical}), nil
}
func validTrustedVehiclePath(path string) bool {
	if path == "" || len(path) > 238 || strings.ContainsAny(path, "\x00\\") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
