package adapter

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Only loopback UDP forwarding is configured automatically. Wildcard listeners
// still forward to loopback; no GCS stream is redirected to a remote machine.
func forwardingTarget(c Config) (string, error) {
	if c.Transport != "udp" {
		return "", fmt.Errorf("automatic GCS setup requires a UDP listener")
	}
	host, port, err := net.SplitHostPort(c.Address)
	if err != nil {
		return "", fmt.Errorf("enter a UDP listen address as host:port")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("enter a UDP port between 1 and 65535")
	}
	ip := net.ParseIP(host)
	if host == "" || host == "0.0.0.0" || host == "localhost" {
		host = "127.0.0.1"
	} else if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("automatic GCS setup requires a loopback UDP address")
	}
	return net.JoinHostPort(host, port), nil
}

func configureGCS(c Config, found []GCS) []GCS {
	for i := range found {
		g := &found[i]
		if g.ID == "mission-planner" && g.SettingsPath != "" {
			if data, err := os.ReadFile(g.SettingsPath); err == nil {
				g.EnhancementState = "unmanaged"
				if bytes.HasPrefix(data, []byte("// OpenUTM adapter managed plugin")) {
					g.EnhancementState = "installed"
					g.EnhancementVersion = "0.1"
				}
			} else if os.IsNotExist(err) {
				g.EnhancementState = "absent"
			}
		}
		if !g.Installed {
			continue
		}
		if g.ID == "mission-planner" && !c.MissionPlannerPlugin {
			g.SetupState, g.SetupDetail = "optional", "Use Mission Planner’s MAVLink mirror, or choose the optional OpenUTM plugin in Ground stations"
			if g.EnhancementState == "installed" {
				g.SetupDetail = "OpenUTM plugin is installed. Remove it in Ground stations to stop plugin forwarding."
			}
			continue
		}
		if !c.AutoConfigureGCS || c.Source == "direct" || (c.Source != "auto" && c.Source != g.ID) {
			g.SetupState, g.SetupDetail = "manual", "Automatic setup is off for this ground station"
			continue
		}
		target, err := forwardingTarget(c)
		if err != nil {
			g.SetupState, g.SetupDetail = "attention", err.Error()
			continue
		}
		if g.SettingsPath == "" {
			g.SetupState, g.SetupDetail = "attention", "Open this ground station once, then close it so its settings can be configured"
			continue
		}
		var data []byte
		var changed bool
		if g.ID == "qgroundcontrol" {
			data, changed, err = qgcForwarding(g.SettingsPath, target)
		} else {
			data = []byte(missionPlannerPlugin(target))
			var previous []byte
			previous, err = os.ReadFile(g.SettingsPath)
			if os.IsNotExist(err) {
				err = nil
			}
			changed = !bytes.Equal(previous, data)
			if len(previous) > 0 && !bytes.HasPrefix(previous, []byte("// OpenUTM adapter managed plugin")) {
				err = fmt.Errorf("existing plugin is not managed by UTM Adapter; leaving it unchanged")
			}
		}
		if err != nil {
			g.SetupState, g.SetupDetail = "attention", err.Error()
			continue
		}
		if !changed {
			if backups, _ := filepath.Glob(g.SettingsPath + ".utm-adapter-backup-*"); len(backups) > 0 {
				g.BackupPath = backups[0]
			}
			g.SetupState, g.SetupDetail = "ready", "Forwarding configured to "+target
			continue
		}
		if g.Running {
			g.SetupState, g.SetupDetail = "close", "Close "+g.Name+"; forwarding will be configured automatically, then reopen it"
			continue
		}
		backup, err := replaceGCSFile(g.SettingsPath, data)
		if err != nil {
			g.SetupState, g.SetupDetail = "attention", "Could not configure forwarding: "+err.Error()
			continue
		}
		if g.ID == "qgroundcontrol" {
			if err := os.WriteFile(g.SettingsPath+".utm-adapter-target", []byte(target), 0600); err != nil {
				g.SetupState, g.SetupDetail = "attention", "Forwarding saved, but could not record managed destination: "+err.Error()
				continue
			}
		}
		if g.ID == "mission-planner" {
			g.EnhancementState = "installed"
			g.EnhancementVersion = "0.1"
		}
		g.SetupState, g.SetupDetail, g.BackupPath = "ready", "Forwarding configured to "+target+". Open "+g.Name+" to connect.", backup
	}
	return found
}

// QSettings stores these root facts in [General]. Preserve every unrelated line
// (including escaped Qt values) rather than round-tripping through an INI library.
func qgcForwarding(path, target string) ([]byte, bool, error) {
	original, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	newline := "\n"
	if bytes.Contains(original, []byte("\r\n")) {
		newline = "\r\n"
	}
	text := strings.ReplaceAll(string(original), "\r\n", "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	section := ""
	values := map[string]string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = line
			continue
		}
		if section == "[General]" {
			k, v, ok := strings.Cut(line, "=")
			if ok {
				values[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), "\"")
			}
		}
	}
	enabled := strings.EqualFold(values["forwardMavlink"], "true") || values["forwardMavlink"] == "1"
	current := values["forwardMavlinkHostName"]
	if enabled && sameTarget(current, target) {
		return original, false, nil
	}
	// An enabled, different destination may be feeding another system. Only update
	// it if it exactly matches our previous managed destination.
	managed, _ := os.ReadFile(path + ".utm-adapter-target")
	if enabled && !sameTarget(current, strings.TrimSpace(string(managed))) {
		return nil, false, fmt.Errorf("forwarding is already enabled to %s; leaving it unchanged", current)
	}
	output := []string{}
	section = ""
	inserted := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = trimmed
			output = append(output, line)
			if section == "[General]" {
				output = append(output, "forwardMavlink=true", "forwardMavlinkHostName="+target)
				inserted = true
			}
			continue
		}
		k, _, ok := strings.Cut(trimmed, "=")
		if section == "[General]" && ok && (strings.TrimSpace(k) == "forwardMavlink" || strings.TrimSpace(k) == "forwardMavlinkHostName") {
			continue
		}
		output = append(output, line)
	}
	if !inserted {
		output = append([]string{"[General]", "forwardMavlink=true", "forwardMavlinkHostName=" + target, ""}, output...)
	}
	return []byte(strings.Join(output, newline) + newline), true, nil
}
func sameTarget(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	normalize := func(s string) string { return strings.ReplaceAll(s, "localhost:", "127.0.0.1:") }
	return normalize(a) == normalize(b)
}
func replaceGCSFile(path string, data []byte) (string, error) {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("settings file is a symbolic link")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	backup := ""
	mode := os.FileMode(0600)
	if original, err := os.ReadFile(path); err == nil {
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		}
		backup = path + ".utm-adapter-backup-" + time.Now().UTC().Format("20060102T150405.000000000Z")
		if err = os.WriteFile(backup, original, 0600); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".utm-adapter-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return "", err
	}
	return backup, nil
}

// Mission Planner's persistent mirror list requires a manual Start click. Its
// supported source-plugin loader provides startup forwarding without UI driving.
func missionPlannerPlugin(target string) string {
	host, port, _ := net.SplitHostPort(target)
	return fmt.Sprintf(`// OpenUTM adapter managed plugin v1; remove this file to uninstall.
using System;
using System.Net;
using System.Net.Sockets;
using MissionPlanner;
namespace OpenUTM {
 public class AdapterForwarder : MissionPlanner.Plugin.Plugin {
  public override string Name { get { return "OpenUTM telemetry forwarder"; } }
  public override string Version { get { return "0.1"; } }
  public override string Author { get { return "OpenUTM"; } }
  private UdpClient udp;
  private MAVLinkInterface source;
  private readonly object gate = new object();
  public override bool Init() { loopratehz = 1; return true; }
  public override bool Loaded() {
   lock(gate) { udp = new UdpClient(AddressFamily.%s); udp.Connect(IPAddress.Parse("%s"), %s); }
   return Loop();
  }
  public override bool Loop() {
   lock(gate) {
    if (source != MainV2.comPort) {
     if (source != null) source.OnPacketReceived -= Forward;
     source = MainV2.comPort;
     if (source != null) source.OnPacketReceived += Forward;
    }
   }
   return true;
  }
  private void Forward(object sender, MAVLink.MAVLinkMessage packet) {
   lock(gate) {
    if (udp == null || packet == null || packet.buffer == null) return;
    try { udp.Send(packet.buffer, packet.buffer.Length); }
    catch (SocketException) { } // Adapter may be paused; never disrupt the GCS.
   }
  }
  public override bool Exit() {
   lock(gate) {
    if (source != null) source.OnPacketReceived -= Forward;
    source = null;
    if (udp != null) udp.Close();
    udp = null;
   }
   return true;
  }
 }
}
`, map[bool]string{true: "InterNetworkV6", false: "InterNetwork"}[strings.Contains(host, ":")], host, port)
}

// Rename to an unloaded backup instead of deleting the user's only copy.
func removeMissionPlannerPlugin(g GCS) (string, error) {
	if g.Running {
		return "", fmt.Errorf("close Mission Planner before removing the plugin")
	}
	if g.SettingsPath == "" {
		return "", fmt.Errorf("Mission Planner installation could not be located")
	}
	data, err := os.ReadFile(g.SettingsPath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !bytes.HasPrefix(data, []byte("// OpenUTM adapter managed plugin")) {
		return "", fmt.Errorf("existing plugin is not managed by UTM Adapter; leaving it unchanged")
	}
	info, err := os.Lstat(g.SettingsPath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("plugin is not a regular file")
	}
	backup := g.SettingsPath + ".utm-adapter-backup-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	if err := os.Rename(g.SettingsPath, backup); err != nil {
		return "", err
	}
	return backup, nil
}
