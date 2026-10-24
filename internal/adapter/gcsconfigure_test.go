package adapter

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutomaticQGCSetup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "QGroundControl.ini")
	original := []byte("[General]\r\nSettingsVersion=9\r\nforwardMavlink=false\r\nforwardMavlinkHostName=localhost:14445\r\n\r\n[Other]\r\nopaque=@ByteArray(abc)\r\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	c := DefaultConfig()
	g := []GCS{{ID: "qgroundcontrol", Name: "QGroundControl", Installed: true, Running: true, SettingsPath: path}}
	result := configureGCS(c, g)
	if result[0].SetupState != "close" {
		t.Fatal(result)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, original) {
		t.Fatal("modified running GCS settings")
	}
	g[0].Running = false
	result = configureGCS(c, g)
	if result[0].SetupState != "ready" || result[0].BackupPath == "" {
		t.Fatal(result)
	}
	backup, _ := os.ReadFile(result[0].BackupPath)
	if !bytes.Equal(backup, original) {
		t.Fatal("backup mismatch")
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "forwardMavlink=true\r\nforwardMavlinkHostName=127.0.0.1:14551\r\n") || !strings.Contains(string(data), "opaque=@ByteArray(abc)\r\n") {
		t.Fatal(string(data))
	}
	g[0].Running = true
	result = configureGCS(c, g)
	if result[0].SetupState != "ready" {
		t.Fatal(result)
	}
	backups, _ := filepath.Glob(path + ".utm-adapter-backup-*")
	if len(backups) != 1 {
		t.Fatal("repeated backup", backups)
	}
	c.Address = "127.0.0.1:14552"
	g[0].Running = false
	result = configureGCS(c, g)
	if result[0].SetupState != "ready" {
		t.Fatal("managed endpoint change", result)
	}
	data, _ = os.ReadFile(path)
	if !bytes.Contains(data, []byte("127.0.0.1:14552")) {
		t.Fatal(string(data))
	}
}
func TestQGCPreservesOtherForwarderAndDisabledSetup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "QGroundControl.ini")
	original := []byte("[General]\nforwardMavlink=true\nforwardMavlinkHostName=192.168.1.8:1234\n")
	os.WriteFile(path, original, 0600)
	c := DefaultConfig()
	g := []GCS{{ID: "qgroundcontrol", Installed: true, SettingsPath: path}}
	result := configureGCS(c, g)
	if result[0].SetupState != "attention" {
		t.Fatal(result)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, original) {
		t.Fatal("overwrote existing forwarding")
	}
	for _, source := range []string{"direct", "mission-planner"} {
		c.Source = source
		result = configureGCS(c, g)
		if result[0].SetupState != "manual" {
			t.Fatal(result)
		}
	}
	c.Source = "auto"
	c.AutoConfigureGCS = false
	result = configureGCS(c, g)
	if result[0].SetupState != "manual" {
		t.Fatal(result)
	}
}
func TestForwardingTargets(t *testing.T) {
	c := DefaultConfig()
	for _, addr := range []string{"127.0.0.1:14551", "localhost:14551", "0.0.0.0:14551", ":14551", "[::1]:14551"} {
		c.Address = addr
		if _, err := forwardingTarget(c); err != nil {
			t.Fatal(addr, err)
		}
	}
	for _, addr := range []string{"192.168.1.2:14551", "127.0.0.1:0", "localhost:abc", "bad"} {
		c.Address = addr
		if _, err := forwardingTarget(c); err == nil {
			t.Fatal(addr)
		}
	}
	c.Transport = "tcp"
	if _, err := forwardingTarget(c); err == nil {
		t.Fatal("TCP accepted")
	}
}
func TestMissionPlannerPluginSetup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plugins", "OpenUTMAdapter.cs")
	c := DefaultConfig()
	c.MissionPlannerPlugin = true
	g := []GCS{{ID: "mission-planner", Name: "Mission Planner", Installed: true, SettingsPath: path}}
	result := configureGCS(c, g)
	if result[0].SetupState != "ready" {
		t.Fatal(result)
	}
	plugin, _ := os.ReadFile(path)
	for _, want := range []string{"OnPacketReceived += Forward", `IPAddress.Parse("127.0.0.1"), 14551`, "udp.Send(packet.buffer, packet.buffer.Length)", "OnPacketReceived -= Forward"} {
		if !bytes.Contains(plugin, []byte(want)) {
			t.Fatal("missing", want)
		}
	}
	if bytes.Contains(plugin, []byte("MirrorStreamWrite")) {
		t.Fatal("should receive passively")
	}
	os.WriteFile(path, []byte("// user plugin"), 0600)
	result = configureGCS(c, g)
	if result[0].SetupState != "attention" {
		t.Fatal("overwrote user plugin", result)
	}
}
func TestQGCWithoutGeneralSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "QGC.ini")
	os.WriteFile(path, []byte("[Other]\nforwardMavlink=false\n"), 0600)
	data, changed, err := qgcForwarding(path, "127.0.0.1:14551")
	if err != nil || !changed || !bytes.HasPrefix(data, []byte("[General]\nforwardMavlink=true\n")) || !bytes.Contains(data, []byte("[Other]\nforwardMavlink=false")) {
		t.Fatal(string(data), err)
	}
}

func TestMissionPlannerEnhancementOptInAndRemoval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plugins", "OpenUTMAdapter.cs")
	g := GCS{ID: "mission-planner", Installed: true, SettingsPath: path}
	c := DefaultConfig()
	found := configureGCS(c, []GCS{g})
	if found[0].SetupState != "optional" {
		t.Fatal(found)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("installed without opt-in")
	}
	c.MissionPlannerPlugin = true
	found = configureGCS(c, []GCS{g})
	if found[0].EnhancementState != "installed" {
		t.Fatal(found)
	}
	g.Running = true
	if _, err := removeMissionPlannerPlugin(g); err == nil {
		t.Fatal("removed plugin while GCS running")
	}
	g.Running = false
	backup, err := removeMissionPlannerPlugin(g)
	if err != nil || backup == "" {
		t.Fatal(backup, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("plugin remains active")
	}
	if data, err := os.ReadFile(backup); err != nil || !bytes.HasPrefix(data, []byte("// OpenUTM adapter managed plugin")) {
		t.Fatal("missing backup", err)
	}
	os.WriteFile(path, []byte("// user's plugin"), 0600)
	if _, err := removeMissionPlannerPlugin(g); err == nil {
		t.Fatal("removed unmanaged plugin")
	}
}
func TestExistingConfigDoesNotOptIntoPlugin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "product.json")
	os.WriteFile(path, []byte(`{"autoConfigureGCS":true}`), 0600)
	c, err := LoadConfig(path)
	if err != nil || c.MissionPlannerPlugin || !c.AutoConfigureGCS {
		t.Fatal(c, err)
	}
}

// Compile the exact generated plugin against an official Mission Planner build.
// CI supplies the Windows framework compiler; local runs can use dotnet + Roslyn.
func TestMissionPlannerPluginCompilation(t *testing.T) {
	compiler := os.Getenv("UTM_TEST_MP_CSC")
	if compiler == "" {
		t.Skip("set UTM_TEST_MP_CSC and UTM_TEST_MP_DIRECTORY for native plugin compilation")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "OpenUTMAdapter.cs")
	if err := os.WriteFile(source, []byte(missionPlannerPlugin("127.0.0.1:14551")), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-nologo", "-target:library", "-langversion:5", "-out:" + filepath.Join(dir, "OpenUTMAdapter.dll")}
	for _, name := range []string{"MissionPlanner.exe", "MissionPlanner.ArduPilot.dll", "MissionPlanner.Comms.dll", "MissionPlanner.Utilities.dll", "MissionPlanner.Controls.dll", "MAVLink.dll", "GMap.NET.Core.dll", "GMap.NET.Drawing.dll", "GMap.NET.WindowsForms.dll", "Newtonsoft.Json.dll"} {
		args = append(args, "-r:"+filepath.Join(os.Getenv("UTM_TEST_MP_DIRECTORY"), name))
	}
	if references := os.Getenv("UTM_TEST_MP_FRAMEWORK"); references != "" {
		args = append(args, "-noconfig", "-nostdlib+")
		for _, name := range []string{"mscorlib.dll", "System.dll", "System.Core.dll", "System.Drawing.dll", "System.Windows.Forms.dll", "System.Xml.dll", "Facades/netstandard.dll"} {
			args = append(args, "-r:"+filepath.Join(references, name))
		}
	} else {
		args = append(args, "-r:System.Drawing.dll", "-r:System.Windows.Forms.dll", "-r:System.Xml.dll")
	}
	args = append(args, source)
	if dotnet := os.Getenv("UTM_TEST_MP_DOTNET"); dotnet != "" {
		args = append([]string{compiler}, args...)
		compiler = dotnet
	}
	output, err := exec.Command(compiler, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("native plugin compilation failed: %v\n%s", err, output)
	}
}
