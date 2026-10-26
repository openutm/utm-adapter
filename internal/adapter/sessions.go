package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const Version = "0.2.0"

func (s *Service) hasActiveSession(system int) bool {
	var count int
	if s.records == nil {
		return false
	}
	_ = s.records.db.QueryRow("SELECT COUNT(*) FROM sessions WHERE system_id=? AND ended=0", system).Scan(&count)
	return count > 0
}

func (s *Service) ensureRecords() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records != nil {
		return nil
	}
	r, err := openRecords(filepath.Join(filepath.Dir(s.path), "records.sqlite"))
	if err != nil {
		return fmt.Errorf("open flight records: %w", err)
	}
	s.records = r
	s.state.Records = linkResult(nil, "Flight records are stored locally")
	return nil
}

// Close is for process shutdown. Stop only pauses connections.
func (s *Service) Close() {
	s.op.Lock()
	defer s.op.Unlock()
	s.stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records != nil {
		_ = s.records.db.Close()
		s.records = nil
	}
}
func (s *Service) BeginSession(a Assignment) (Session, error) {
	s.op.Lock()
	defer s.op.Unlock()
	if err := s.ensureRecords(); err != nil {
		return Session{}, err
	}
	s.mu.Lock()
	c := s.config
	s.mu.Unlock()
	if c.SystemID != 0 && c.SystemID != a.SystemID {
		return Session{}, fmt.Errorf("receiver system filter excludes this session; select system 0 to receive multiple vehicles")
	}
	return s.records.begin(a, c)
}
func (s *Service) EndSession(id string) error {
	s.op.Lock()
	defer s.op.Unlock()
	if err := s.ensureRecords(); err != nil {
		return err
	}
	return s.records.end(id)
}
func (s *Service) GetDeclarations() ([]Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records == nil {
		return []Document{}, nil
	}
	return s.records.documents(s.config.BaseURL, "declaration")
}

// Export is streamed from SQLite, so a long flight does not have to fit in RAM.
func (s *Service) ExportSession(id string) (string, error) {
	s.op.Lock()
	defer s.op.Unlock()
	if !validUUID(id) {
		return "", fmt.Errorf("invalid session ID")
	}
	if err := s.ensureRecords(); err != nil {
		return "", err
	}
	dir := filepath.Join(filepath.Dir(s.path), "exports")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "report-*.json")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return "", err
	}
	write := func() error {
		var raw string
		var revision int64
		if err := s.records.db.QueryRow("SELECT data,revision FROM sessions WHERE id=?", id).Scan(&raw, &revision); err != nil {
			return err
		}
		var session Session
		if err := json.Unmarshal([]byte(raw), &session); err != nil {
			return err
		}
		if session.EndedAt == nil {
			return fmt.Errorf("end the session before exporting its final report")
		}
		session.Revision = revision
		b, _ := json.Marshal(session)
		history := []json.RawMessage{}
		docs, err := s.records.db.Query("SELECT data FROM documents WHERE server=? AND kind='declaration' AND id=? ORDER BY rowid", session.BlenderURL, session.Assignment.DeclarationID)
		if err != nil {
			return err
		}
		for docs.Next() {
			var data string
			if err = docs.Scan(&data); err != nil {
				docs.Close()
				return err
			}
			history = append(history, json.RawMessage(data))
		}
		err = docs.Err()
		docs.Close()
		if err != nil {
			return err
		}
		declarations, _ := json.Marshal(history)
		if _, err := fmt.Fprintf(f, "{\"schemaVersion\":1,\"session\":%s,\"declarationHistory\":%s,\"samples\":[", b, declarations); err != nil {
			return err
		}
		rows, err := s.records.db.Query("SELECT sequence,data FROM samples WHERE session_id=? ORDER BY sequence", id)
		if err != nil {
			return err
		}
		comma := ""
		for rows.Next() {
			var n int64
			if err = rows.Scan(&n, &raw); err != nil {
				rows.Close()
				return err
			}
			if _, err = fmt.Fprintf(f, "%s{\"sequence\":%d,\"vehicle\":%s}", comma, n, raw); err != nil {
				rows.Close()
				return err
			}
			comma = ","
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if _, err = io.WriteString(f, "],\"audit\":["); err != nil {
			return err
		}
		rows, err = s.records.db.Query("SELECT at,action FROM audit WHERE session_id=? ORDER BY sequence", id)
		if err != nil {
			return err
		}
		defer rows.Close()
		comma = ""
		for rows.Next() {
			var at, action string
			if err = rows.Scan(&at, &action); err != nil {
				return err
			}
			b, _ := json.Marshal(map[string]string{"at": at, "action": action})
			if _, err = fmt.Fprintf(f, "%s%s", comma, b); err != nil {
				return err
			}
			comma = ","
		}
		if err = rows.Err(); err != nil {
			return err
		}
		_, err = io.WriteString(f, "]}\n")
		return err
	}
	err = write()
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, id+"-"+newID()+".json")
	if err = os.Rename(f.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}

// Synchronize retries durable deliveries without starting a telemetry receiver.
func (s *Service) Synchronize() error {
	s.op.Lock()
	defer s.op.Unlock()
	if err := s.ensureRecords(); err != nil {
		return err
	}
	s.mu.Lock()
	c := s.config
	s.mu.Unlock()
	s.synchronize(context.Background(), c)
	return nil
}
func (s *Service) synchronize(parent context.Context, c Config) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	ctx, cancel := context.WithTimeout(parent, requestBudget)
	defer cancel()
	s.syncRecords(ctx, NewBlender(c), NewAerobridge(c), c)
}

func (s *Service) syncRecords(ctx context.Context, b *Blender, fleet *Aerobridge, c Config) {
	err := b.captureDocuments(ctx, s.records)
	s.mu.Lock()
	s.state.Records = linkResult(err, "Flight declarations cached; session records synchronizing")
	s.mu.Unlock()
	err = s.refreshRestrictions(ctx, b)
	s.mu.Lock()
	s.state.Restrictions = linkResult(err, "Airspace restrictions refreshed")
	s.mu.Unlock()
	if c.AerobridgeEnabled {
		err = fleet.Health(ctx)
		s.mu.Lock()
		s.state.Aerobridge = linkResult(err, "Aerobridge registry access verified")
		s.mu.Unlock()
	} else {
		s.mu.Lock()
		s.state.Aerobridge = Link{State: "idle", Detail: "Optional fleet connection is disabled"}
		s.mu.Unlock()
	}
	records, err := s.records.list(-1)
	if err != nil {
		s.recordError(err)
		return
	}
	for _, r := range records {
		if ctx.Err() != nil {
			return
		}
		if r.BlenderPending && r.Session.BlenderURL == strings.TrimRight(c.BaseURL, "/") {
			batch, e := s.records.batch(r.Session.ID, "blender")
			if e == nil {
				var payload archiveBatch
				payload, e = s.records.archive(batch)
				if e == nil {
					var response []byte
					response, e = b.request(ctx, "PUT", "/adapter_ops/sessions/"+r.Session.ID, payload, true)
					if e == nil {
						e = validateArchiveReceipt(response, payload)
					}
				}
			}
			if e == nil {
				e = s.records.ack(batch, "blender")
			}
			if e != nil {
				s.recordError(e)
			}
		}
		if c.AerobridgeEnabled && r.AerobridgePending && r.Session.AerobridgeURL == strings.TrimRight(c.AerobridgeURL, "/") {
			batch, e := s.records.batch(r.Session.ID, "aerobridge")
			if e == nil {
				e = fleet.Log(ctx, batch)
			}
			if e == nil {
				e = s.records.ack(batch, "aerobridge")
			}
			if e != nil {
				s.mu.Lock()
				s.state.Aerobridge = linkResult(e, "")
				s.mu.Unlock()
			}
		}
	}
}
func (s *Service) recordError(err error) {
	s.mu.Lock()
	s.state.Records = linkResult(err, "")
	s.mu.Unlock()
}

// Session delivery uses the immutable assignment captured at flight start.
// Archived telemetry is uploaded as a compliance log, never replayed as live RID.
func (s *Service) publishSessions(ctx context.Context, b *Blender, c Config) {
	if !c.SessionDelivery {
		return
	}
	records, err := s.records.list(-1)
	if err != nil {
		s.recordError(err)
		return
	}
	for _, r := range records {
		if r.Session.EndedAt != nil || r.Session.BlenderURL != strings.TrimRight(c.BaseURL, "/") {
			continue
		}
		a := r.Session.Assignment
		s.mu.Lock()
		v, ok := s.vehicles[byte(a.SystemID)]
		s.mu.Unlock()
		if !ok || time.Since(v.GPSAt) > 5*time.Second || time.Since(v.HeartbeatAt) > 5*time.Second {
			continue
		}
		// Deduplication is connection-local. The durable archive has its own cursor.
		s.mu.Lock()
		last := s.sessionSent[r.Session.ID]
		s.mu.Unlock()
		if !v.GPSAt.After(last) {
			continue
		}
		bc := c
		bc.SystemID = a.SystemID
		bc.DeclarationID = a.DeclarationID
		bc.VehicleID = a.VehicleID
		bc.OperatorID = a.OperatorID
		err = b.publishFor(ctx, v, bc)
		s.mu.Lock()
		s.state.Delivery = linkResult(err, "Session telemetry accepted by Flight Blender")
		if err == nil {
			s.sessionSent[r.Session.ID] = v.GPSAt
			s.state.Sent++
		}
		s.mu.Unlock()
	}
}
