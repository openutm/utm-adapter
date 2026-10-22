package adapter

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	BaseURL              string `json:"baseURL"`
	AuthMode             string `json:"authMode"`
	Token                string `json:"token,omitempty"`
	TokenURL             string `json:"tokenURL"`
	ClientID             string `json:"clientID"`
	ClientSecret         string `json:"clientSecret,omitempty"`
	Scope                string `json:"scope"`
	MissionPlannerPlugin bool   `json:"missionPlannerPlugin"`
	AutoConfigureGCS     bool   `json:"autoConfigureGCS"`
	Source               string `json:"source"`
	Transport            string `json:"transport"`
	Address              string `json:"address"`
	SerialDevice         string `json:"serialDevice"`
	Baud                 int    `json:"baud"`
	SystemID             int    `json:"systemID"`
	DeclarationID        string `json:"declarationID"`
	VehicleID            string `json:"vehicleID"`
	OperatorID           string `json:"operatorID"`
	Publish              bool   `json:"publish"`
	HasToken             bool   `json:"hasToken"`
	HasClientSecret      bool   `json:"hasClientSecret"`
}

func DefaultConfig() Config {
	return Config{BaseURL: "http://127.0.0.1:8000", AuthMode: "bearer", Scope: "flightblender.read flightblender.write", Source: "auto", AutoConfigureGCS: true, Transport: "udp", Address: "127.0.0.1:14551", Baud: 57600, OperatorID: "utm-adapter"}
}
func validURL(value string) bool {
	u, e := url.Parse(value)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}
func (c Config) Validate() error {
	if !validURL(c.BaseURL) {
		return fmt.Errorf("enter a valid Flight Blender HTTP(S) URL without credentials or query parameters")
	}
	if c.AuthMode != "none" && c.AuthMode != "bearer" && c.AuthMode != "oauth" {
		return fmt.Errorf("unknown authentication mode")
	}
	if c.AuthMode == "oauth" && (!validURL(c.TokenURL) || c.ClientID == "") {
		return fmt.Errorf("OAuth requires a token URL and client ID")
	}
	if c.Source != "auto" && c.Source != "qgroundcontrol" && c.Source != "mission-planner" && c.Source != "direct" {
		return fmt.Errorf("unknown telemetry source")
	}
	if c.Transport != "udp" && c.Transport != "tcp" && c.Transport != "serial" {
		return fmt.Errorf("unknown transport")
	}
	if c.Transport == "serial" {
		if c.SerialDevice == "" || c.Baud < 1200 {
			return fmt.Errorf("enter a serial device and baud rate")
		}
	} else if c.Address == "" {
		return fmt.Errorf("enter a host:port address")
	}
	if c.SystemID < 0 || c.SystemID > 255 {
		return fmt.Errorf("MAVLink system ID must be between 0 and 255")
	}
	if c.Publish && (c.DeclarationID == "" || c.VehicleID == "" || c.SystemID == 0 || c.OperatorID == "") {
		return fmt.Errorf("publishing requires a declaration UUID, vehicle serial, operator ID and explicit MAVLink system ID")
	}
	if c.DeclarationID != "" {
		s := c.DeclarationID
		if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' || strings.Trim(s, "0123456789abcdefABCDEF-") != "" {
			return fmt.Errorf("declaration ID must be a UUID")
		}
	}
	return nil
}
func LoadConfig(path string) (Config, error) {
	c := DefaultConfig()
	b, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	c.Token = ""
	c.ClientSecret = ""
	return c, e
}
func SaveConfig(path string, c Config) error {
	c.Token = ""
	c.ClientSecret = ""
	c.HasToken = false
	c.HasClientSecret = false
	b, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), "product-*.json")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(f.Name(), path)
}
