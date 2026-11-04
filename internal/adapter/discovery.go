package adapter

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type GCS struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Installed          bool   `json:"installed"`
	Running            bool   `json:"running"`
	SettingsPath       string `json:"settingsPath"`
	SetupState         string `json:"setupState"`
	SetupDetail        string `json:"setupDetail"`
	EnhancementState   string `json:"enhancementState"`
	EnhancementVersion string `json:"enhancementVersion"`
	BackupPath         string `json:"backupPath"`
}

func discoverBuiltin(ctx context.Context, id string) GCS {
	var out []byte
	if runtime.GOOS == "windows" {
		out, _ = exec.CommandContext(ctx, "tasklist", "/FO", "CSV", "/NH").Output()
	} else {
		out, _ = exec.CommandContext(ctx, "ps", "-A", "-o", "comm=").Output()
	}
	processes := strings.ToLower(string(out))
	home, _ := os.UserHomeDir()
	g := GCS{ID: id, Name: "QGroundControl"}
	if id == "mission-planner" {
		g.Name = "Mission Planner"
	}
	needle := "qgroundcontrol"
	if id == "mission-planner" {
		needle = "missionplanner"
	}
	g.Running = strings.Contains(processes, needle)
	if id == "mission-planner" {
		g.Running = g.Running || strings.Contains(processes, "mission planner")
	}
	paths := []string{}
	if id == "qgroundcontrol" {
		paths = []string{"/Applications/QGroundControl.app", filepath.Join(home, "Applications/QGroundControl.app"), filepath.Join(os.Getenv("ProgramFiles"), "QGroundControl", "QGroundControl.exe"), filepath.Join(home, "QGroundControl.AppImage")}
		_, e := exec.LookPath("QGroundControl")
		g.Installed = e == nil
	} else {
		paths = []string{filepath.Join(os.Getenv("ProgramFiles(x86)"), "Mission Planner", "MissionPlanner.exe"), filepath.Join(os.Getenv("ProgramFiles"), "Mission Planner", "MissionPlanner.exe")}
	}
	for _, p := range paths {
		if _, e := os.Stat(p); e == nil {
			g.Installed = true
		}
	}
	g.Installed = g.Installed || g.Running
	if id == "qgroundcontrol" {
		roots := []string{filepath.Join(home, ".config"), os.Getenv("APPDATA"), os.Getenv("LOCALAPPDATA")}
		if root := os.Getenv("XDG_CONFIG_HOME"); root != "" {
			roots = append([]string{root}, roots...)
		}
		for _, root := range roots {
			if root == "" {
				continue
			}
			for _, org := range []string{"qgroundcontrol.com", "QGroundControl.org", "QGroundControl"} {
				candidate := filepath.Join(root, org, "QGroundControl.ini")
				if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && g.SettingsPath == "" {
					g.SettingsPath = candidate
					g.Installed = true
				}
			}
		}
	} else {
		if exe, err := exec.LookPath("MissionPlanner.exe"); err == nil {
			paths = append(paths, exe)
		}
		for _, exe := range paths {
			if info, err := os.Stat(exe); err == nil && !info.IsDir() {
				g.SettingsPath = filepath.Join(filepath.Dir(exe), "plugins", "OpenUTMAdapter.cs")
				g.Installed = true
				break
			}
		}
	}
	return g
}
