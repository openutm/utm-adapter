package adapter

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// TrustedConfig is deliberately separate from the passive adapter configuration.
type TrustedConfig struct {
	Enabled           bool               `json:"enabled"`
	ServerURL         string             `json:"server_url"`
	TimeoutSeconds    int                `json:"timeout_seconds"`
	AutoConnectDrone  bool               `json:"auto_connect_drone"`
	AutoConnectServer bool               `json:"auto_connect_server"`
	WatchDirectory    string             `json:"watch_directory"`
	FlightPassport    PassportConfig     `json:"flight_passport"`
	Credentials       TrustedCredentials `json:"credentials"`
}
type PassportConfig struct {
	URL          string `json:"url"`
	Domain       string `json:"domain"`
	TokenURL     string `json:"token_url"`
	Audience     string `json:"audience"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}
type TrustedCredentials struct {
	OrganizationID string `json:"organization_id"`
	OperatorID     string `json:"operator_id"`
	DefaultPilotID string `json:"default_pilot_id"`
}

func trustedConfigPath(path string) string {
	return filepath.Join(filepath.Dir(path), "aerobridge_config.json")
}
func loadTrustedConfig(path string) (TrustedConfig, error) {
	c := TrustedConfig{TimeoutSeconds: 30, FlightPassport: PassportConfig{TokenURL: "/oauth/token/"}}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	var wrapper struct {
		Aerobridge json.RawMessage `json:"aerobridge"`
	}
	if err = json.Unmarshal(raw, &wrapper); err != nil {
		return c, err
	}
	if len(wrapper.Aerobridge) == 0 || string(wrapper.Aerobridge) == "null" {
		return c, fmt.Errorf("missing aerobridge configuration")
	}
	if err = json.Unmarshal(wrapper.Aerobridge, &c); err != nil {
		return c, err
	}
	return c, c.validate()
}
func (c TrustedConfig) validate() error {
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 300 {
		return fmt.Errorf("timeout_seconds must be 1–300")
	}
	if !c.Enabled {
		return nil
	}
	for _, v := range []string{c.ServerURL, c.FlightPassport.URL} {
		u, e := url.Parse(v)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("invalid Aerobridge or Passport URL")
		}
	}
	u, e := url.Parse(c.FlightPassport.TokenURL)
	if e != nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("Passport token_url must be a relative endpoint")
	}
	if c.FlightPassport.ClientID == "" || c.FlightPassport.ClientSecret == "" || c.FlightPassport.Audience == "" {
		return fmt.Errorf("Passport client ID, secret and audience are required")
	}
	if !validUUID(c.Credentials.OrganizationID) || !validUUID(c.Credentials.OperatorID) {
		return fmt.Errorf("organization and operator UUIDs are required")
	}
	return nil
}
func saveTrustedConfig(path string, c TrustedConfig) error {
	if err := c.validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(struct {
		Aerobridge TrustedConfig `json:"aerobridge"`
	}{c}, "", "  ")
	if err != nil {
		return err
	}
	return atomicTrustedFile(path, raw)
}
func atomicTrustedFile(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".aerobridge-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
