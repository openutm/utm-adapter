package adapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// runTrusted is independent of Flight Blender delivery. TryLock ensures shutdown
// (which owns op) can always cancel and join this worker without a lock cycle.
func (s *Service) runTrusted(ctx context.Context) {
	if s.trusted == nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	nextConnect := time.Time{}
	nextHealth := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if !s.op.TryLock() {
			continue
		}
		t := s.trusted
		t.mu.Lock()
		if t.config.Enabled {
			if t.api != nil && t.status.Ready && time.Now().After(nextHealth) {
				nextHealth = time.Now().Add(10 * time.Second)
				budget, cancel := context.WithTimeout(ctx, time.Duration(t.config.TimeoutSeconds)*time.Second)
				_, err := t.api.request(budget, "GET", "/ping/", nil, false)
				cancel()
				if err != nil {
					t.status.Ready = false
					t.status.AircraftID = ""
					t.status.PilotID = ""
					t.status.Server = linkResult(err, "")
					trustedFailure(t, err)
				}
			}
			if !s.trustedFresh(t) && t.config.AutoConnectDrone && t.config.AutoConnectServer && time.Now().After(nextConnect) {
				nextConnect = time.Now().Add(30 * time.Second)
				s.mu.Lock()
				system := byte(0)
				count := 0
				for id, v := range s.vehicles {
					if time.Since(v.HeartbeatAt) < 5*time.Second {
						system = id
						count++
					}
				}
				s.mu.Unlock()
				if count == 1 {
					_, _ = s.connectTrusted(ctx, t, int(system))
				}
			}
			if !t.status.Ready && t.status.AircraftID != "" && t.config.Credentials.DefaultPilotID != "" {
				_ = s.selectTrustedPilot(t, t.config.Credentials.DefaultPilotID)
			}
			if t.status.Ready && s.trustedFresh(t) && t.config.WatchDirectory != "" {
				budget, cancel := context.WithTimeout(ctx, time.Duration(t.config.TimeoutSeconds)*time.Second)
				err := s.watchTrustedPlans(budget, t)
				cancel()
				if err != nil {
					trustedFailure(t, err)
				}
			}
		}
		t.mu.Unlock()
		s.op.Unlock()
	}
}
func (s *Service) watchTrustedPlans(ctx context.Context, t *trustedModule) error {
	entries, err := os.ReadDir(t.config.WatchDirectory)
	if err != nil {
		return fmt.Errorf("watch plans: %w", err)
	}
	if t.watchSeen == nil {
		t.watchSeen = map[string]time.Time{}
	}
	var failures []error
	for _, entry := range entries {
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".plan") {
			continue
		}
		path := filepath.Join(t.config.WatchDirectory, entry.Name())
		info, e := os.Lstat(path)
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			continue
		}
		modified := info.ModTime()
		previous, seen := t.watchSeen[path]
		t.watchSeen[path] = modified
		// Observe the same revision twice to avoid reading a partially saved GCS file.
		if !seen || !previous.Equal(modified) || time.Since(modified) < 2*time.Second {
			continue
		}
		// Persist the scheduled interval before any network call, keeping retry IDs
		// stable even across restart or a lost POST response.
		key := "watch:" + trustedScope(t) + ":" + t.status.AircraftID + ":" + t.status.PilotID + ":" + path + ":" + modified.UTC().Format(time.RFC3339Nano)
		schedule := t.jobs[key]
		var start, end time.Time
		if schedule.Stage != "" {
			parts := strings.Split(schedule.Stage, "|")
			if len(parts) == 2 {
				start, _ = time.Parse(time.RFC3339, parts[0])
				end, _ = time.Parse(time.RFC3339, parts[1])
			}
		}
		if schedule.OperationID != "" {
			continue
		}
		if start.IsZero() {
			start = time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second)
			end = start.Add(time.Hour)
			schedule.Stage = start.Format(time.RFC3339) + "|" + end.Format(time.RFC3339)
			t.jobs[key] = schedule
			if e = s.persistTrustedJobs(t); e != nil {
				return e
			}
		}
		job, e := s.importTrustedPlan(ctx, t, path, start, end)
		if e != nil {
			var remote *aerobridgeError
			if errors.As(e, &remote) || ctx.Err() != nil {
				return fmt.Errorf("import %s: %w", entry.Name(), e)
			}
			failures = append(failures, fmt.Errorf("import %s: %w", entry.Name(), e))
			continue
		}
		schedule.PlanID = job.PlanID
		schedule.OperationID = job.OperationID
		t.jobs[key] = schedule
		if e = s.persistTrustedJobs(t); e != nil {
			return e
		}
		t.status.Detail = "Uploaded " + entry.Name() + " and created operation " + job.OperationID
	}
	return errors.Join(failures...)
}
