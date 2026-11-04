package adapter

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
)

type thirdGCS struct{ path string }

func (thirdGCS) ID() string { return "test-gcs" }
func (p thirdGCS) Discover(context.Context) GCS {
	return GCS{Name: "Test GCS", Installed: true, SettingsPath: p.path}
}
func (p thirdGCS) Configure(c Config, g GCS) GCS {
	target, _ := forwardingTarget(c)
	backup, err := replaceGCSFile(g.SettingsPath, []byte(target))
	if err != nil {
		g.SetupState, g.SetupDetail = "attention", err.Error()
		return g
	}
	g.SetupState, g.SetupDetail, g.BackupPath = "ready", target, backup
	return g
}

// A third integration registers without changes to config validation, discovery,
// setup dispatch, MAVLink parsing, session records or HTTP delivery.
func TestThirdGCSIntegration(t *testing.T) {
	p := thirdGCS{filepath.Join(t.TempDir(), "forwarding.conf")}
	if err := os.WriteFile(p.path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RegisterGCSIntegration(p); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		gcsRegistry.Lock()
		defer gcsRegistry.Unlock()
		for i, item := range gcsRegistry.items {
			if item.ID() == p.ID() {
				gcsRegistry.items = append(gcsRegistry.items[:i], gcsRegistry.items[i+1:]...)
				break
			}
		}
	})
	if err := RegisterGCSIntegration(p); err == nil {
		t.Fatal("duplicate ID accepted")
	}
	c := DefaultConfig()
	c.Source = p.ID()
	c.Address = freeUDP(t)
	c.BridgeAddress = ""
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	var discovered GCS
	for _, g := range Discover() {
		if g.ID == p.ID() {
			discovered = g
		}
	}
	if discovered.Name != "Test GCS" {
		t.Fatal(discovered)
	}
	for _, mode := range []string{"direct", "qgroundcontrol", "disabled", "running", "tcp"} {
		cfg, g := c, discovered
		switch mode {
		case "disabled":
			cfg.AutoConfigureGCS = false
		case "running":
			g.Running = true
		case "tcp":
			cfg.Transport = "tcp"
		default:
			cfg.Source = mode
		}
		result := configureGCS(cfg, []GCS{g})[0]
		if result.SetupState == "ready" {
			t.Fatalf("configured in %s mode", mode)
		}
		raw, _ := os.ReadFile(p.path)
		if string(raw) != "original" {
			t.Fatalf("modified settings in %s mode", mode)
		}
	}
	result := configureGCS(c, []GCS{discovered})[0]
	if result.SetupState != "ready" || result.BackupPath == "" {
		t.Fatal(result)
	}
	raw, _ := os.ReadFile(result.BackupPath)
	if string(raw) != "original" {
		t.Fatal("original not backed up")
	}
	s := testService(t, c)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	session, err := s.BeginSession(assignment(1))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("udp", c.Address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.Write(packets(t, 1, heartbeat())); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Write(packets(t, 1, gps())); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { rows, e := s.records.list(10); return e == nil && len(rows) == 1 && rows[0].Samples == 1 })
	if err := s.EndSession(session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExportSession(session.ID); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownGCSDoesNotWriteMissionPlannerPlugin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	if err := os.WriteFile(path, []byte("unrelated settings"), 0600); err != nil {
		t.Fatal(err)
	}
	result := configureGCS(DefaultConfig(), []GCS{{ID: "unknown", Installed: true, SettingsPath: path}})[0]
	if result.SetupState != "attention" {
		t.Fatal(result)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "unrelated settings" {
		t.Fatal("unknown GCS settings overwritten")
	}
	c := DefaultConfig()
	c.Source = "unknown"
	if c.Validate() == nil {
		t.Fatal("unknown source accepted")
	}
	for _, id := range []string{"auto", "direct", "qgroundcontrol", "Bad ID"} {
		if err := RegisterGCSIntegration(invalidGCS{id}); err == nil {
			t.Fatalf("accepted %q", id)
		}
	}
}

type invalidGCS struct{ id string }

func (p invalidGCS) ID() string                  { return p.id }
func (invalidGCS) Discover(context.Context) GCS  { return GCS{} }
func (invalidGCS) Configure(_ Config, g GCS) GCS { return g }
