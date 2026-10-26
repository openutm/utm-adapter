package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Blender struct {
	config  Config
	client  *http.Client
	mu      sync.Mutex
	token   string
	expires time.Time
}

func NewBlender(c Config) *Blender {
	return &Blender{config: c, client: &http.Client{Timeout: 6 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (b *Blender) accessToken(ctx context.Context) (string, error) {
	if b.config.AuthMode == "none" {
		return "", nil
	}
	if b.config.AuthMode == "bearer" {
		if b.config.Token == "" {
			return "", fmt.Errorf("add a bearer token in Connection settings")
		}
		return b.config.Token, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.token != "" && time.Now().Before(b.expires) {
		return b.token, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}, "scope": {b.config.Scope}}
	req, e := http.NewRequestWithContext(ctx, "POST", b.config.TokenURL, strings.NewReader(form.Encode()))
	if e != nil {
		return "", e
	}
	req.SetBasicAuth(b.config.ClientID, b.config.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, e := b.client.Do(req)
	if e != nil {
		return "", fmt.Errorf("OAuth endpoint unreachable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("OAuth returned HTTP %d", res.StatusCode)
	}
	var data struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&data) != nil || data.AccessToken == "" {
		return "", fmt.Errorf("OAuth response has no access token")
	}
	if data.ExpiresIn == 0 {
		data.ExpiresIn = 300
	}
	if data.ExpiresIn < 0 {
		return "", fmt.Errorf("OAuth token has invalid expiry")
	}
	b.token = data.AccessToken
	b.expires = time.Now().Add(time.Duration(data.ExpiresIn)*time.Second - 30*time.Second)
	return b.token, nil
}
func (b *Blender) request(ctx context.Context, method, path string, payload any, auth bool) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		v, e := json.Marshal(payload)
		if e != nil {
			return nil, e
		}
		body = bytes.NewReader(v)
	}
	req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(b.config.BaseURL, "/")+path, body)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth {
		token, e := b.accessToken(ctx)
		if e != nil {
			return nil, e
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	res, e := b.client.Do(req)
	if e != nil {
		return nil, fmt.Errorf("Flight Blender unreachable; check the URL and server")
	}
	defer res.Body.Close()
	data, e := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
	if len(data) > 8<<20 {
		return nil, fmt.Errorf("API response exceeds 8 MiB limit")
	}
	if e != nil {
		return nil, e
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		if res.StatusCode == 401 {
			b.mu.Lock()
			b.token = ""
			b.mu.Unlock()
		}
		hint := ""
		if res.StatusCode == 401 {
			hint = " — check credentials"
		}
		if res.StatusCode == 400 && path == "/flight_stream/set_telemetry" {
			hint = " — check the declaration time window, operational state and vehicle details"
		}
		if res.StatusCode == 403 {
			hint = " — check flightblender.read / flightblender.write scopes"
		}
		return nil, fmt.Errorf("%s %s: HTTP %d%s", method, path, res.StatusCode, hint)
	}
	return data, nil
}
func (b *Blender) Health(ctx context.Context) error {
	_, e := b.request(ctx, "GET", "/ping", nil, false)
	return e
}
func (b *Blender) CheckAccess(ctx context.Context) error {
	path := "/flight_declaration_ops/flight_declaration?page_size=1"
	if b.config.DeclarationID != "" {
		path = "/flight_declaration_ops/flight_declaration/" + b.config.DeclarationID
	}
	data, e := b.request(ctx, "GET", path, nil, true)
	if e != nil {
		return e
	}
	if !json.Valid(data) {
		return fmt.Errorf("Flight Blender API returned invalid JSON")
	}
	return nil
}
func (b *Blender) Publish(ctx context.Context, v Vehicle) error {
	return b.publishFor(ctx, v, b.config)
}
func (b *Blender) publishFor(ctx context.Context, v Vehicle, c Config) error {
	if v.AltitudeWGS84 == nil || v.Latitude == nil || v.Longitude == nil || v.Speed == nil || v.Heading == nil {
		return fmt.Errorf("waiting for a GPS fix with WGS84 altitude, speed and course")
	}
	// Armed state alone does not establish whether the vehicle is airborne.
	status := "Undeclared"
	state := map[string]any{"timestamp": map[string]any{"value": v.PositionAt.UTC().Format(time.RFC3339Nano)}, "timestamp_accuracy": 0, "position": map[string]any{"lat": *v.Latitude, "lng": *v.Longitude, "alt": *v.AltitudeWGS84, "accuracy_h": "HAUnknown", "accuracy_v": "VAUnknown", "height": nil, "pressure_altitude": -1000, "extrapolated": false}, "speed_accuracy": "SAUnknown", "operational_status": status, "speed": *v.Speed, "track": *v.Heading, "vertical_speed": 63}
	payload := map[string]any{"observations": []any{map[string]any{"current_states": []any{state}, "flight_details": map[string]any{"id": c.DeclarationID, "operator_id": c.OperatorID, "operation_description": "UTM Adapter MAVLink telemetry", "uas_id": map[string]any{"serial_number": c.VehicleID}}}}}
	_, e := b.request(ctx, "PUT", "/flight_stream/set_telemetry", payload, true)
	return e
}
