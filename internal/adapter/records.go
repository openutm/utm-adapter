package adapter

// The database is deliberately independent of the MAVLink connection lifecycle.
// Pausing/reconfiguring the receiver must never end a flight or delete its log.
import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

type Pilot struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	License string `json:"license"`
	Contact string `json:"contact"`
}
type Assignment struct {
	SystemID              int    `json:"systemID"`
	DeclarationID         string `json:"declarationID"`
	VehicleID             string `json:"vehicleID"`
	OperatorID            string `json:"operatorID"`
	Pilot                 Pilot  `json:"pilot"`
	AerobridgeOperationID string `json:"aerobridgeOperationID"`
}

func (a Assignment) validate() error {
	for _, text := range []string{a.VehicleID, a.OperatorID, a.Pilot.Name, a.Pilot.License, a.Pilot.Contact} {
		if utf8.RuneCountInString(text) > 512 {
			return fmt.Errorf("session fields must be at most 512 characters")
		}
	}
	if utf8.RuneCountInString(a.Pilot.ID) > 128 {
		return fmt.Errorf("pilot ID must be at most 128 characters")
	}

	if a.SystemID < 1 || a.SystemID > 255 || !validUUID(a.DeclarationID) || strings.TrimSpace(a.VehicleID) == "" || strings.TrimSpace(a.OperatorID) == "" {
		return fmt.Errorf("session requires system ID 1–255, declaration UUID, vehicle and operator")
	}
	if strings.TrimSpace(a.Pilot.Name) == "" || strings.TrimSpace(a.Pilot.License) == "" || strings.TrimSpace(a.Pilot.Contact) == "" {
		return fmt.Errorf("pilot name, licence and contact are required")
	}
	if a.AerobridgeOperationID != "" && !validUUID(a.AerobridgeOperationID) {
		return fmt.Errorf("Aerobridge operation must be a UUID")
	}
	return nil
}
func validUUID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	return err == nil
}
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

type Session struct {
	ID            string     `json:"id"`
	Assignment    Assignment `json:"assignment"`
	StartedAt     time.Time  `json:"startedAt"`
	EndedAt       *time.Time `json:"endedAt"`
	BlenderURL    string     `json:"blenderURL"`
	AerobridgeURL string     `json:"aerobridgeURL"`
	Revision      int64      `json:"revision"`
}
type Sample struct {
	Sequence int64   `json:"sequence"`
	Vehicle  Vehicle `json:"vehicle"`
}
type SessionBatch struct {
	Session Session  `json:"session"`
	Samples []Sample `json:"samples"`
}
type RecordSummary struct {
	Session           Session `json:"session"`
	Samples           int64   `json:"samples"`
	BlenderPending    bool    `json:"blenderPending"`
	AerobridgePending bool    `json:"aerobridgePending"`
}
type Document struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Server     string          `json:"server"`
	CapturedAt time.Time       `json:"capturedAt"`
	Data       json.RawMessage `json:"data"`
}
type Records struct{ db *sql.DB }

func openRecords(path string) (*Records, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	// Pre-create with private permissions; SQLite's default honours only umask.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var schemaVersion int
	if err = db.QueryRow("PRAGMA user_version").Scan(&schemaVersion); err != nil {
		db.Close()
		return nil, err
	}
	if schemaVersion > 1 {
		db.Close()
		return nil, fmt.Errorf("flight records require a newer adapter (schema %d)", schemaVersion)
	}
	_, err = db.Exec(`PRAGMA foreign_keys=ON; PRAGMA journal_mode=DELETE; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS sessions(id TEXT PRIMARY KEY, data TEXT NOT NULL, system_id INTEGER NOT NULL, ended INTEGER NOT NULL DEFAULT 0, revision INTEGER NOT NULL DEFAULT 1, fb_revision INTEGER NOT NULL DEFAULT 0, fb_sequence INTEGER NOT NULL DEFAULT 0, ab_revision INTEGER NOT NULL DEFAULT 0, ab_sequence INTEGER NOT NULL DEFAULT 0);
CREATE UNIQUE INDEX IF NOT EXISTS active_system ON sessions(system_id) WHERE ended=0;
CREATE TABLE IF NOT EXISTS samples(session_id TEXT NOT NULL REFERENCES sessions(id), sequence INTEGER NOT NULL, data TEXT NOT NULL, PRIMARY KEY(session_id,sequence));
CREATE TABLE IF NOT EXISTS documents(server TEXT NOT NULL,kind TEXT NOT NULL,id TEXT NOT NULL,digest TEXT NOT NULL,captured TEXT NOT NULL,data TEXT NOT NULL,observation INTEGER PRIMARY KEY AUTOINCREMENT);
CREATE TABLE IF NOT EXISTS deliveries(session_id TEXT NOT NULL,destination TEXT NOT NULL,data TEXT NOT NULL,PRIMARY KEY(session_id,destination));
CREATE TABLE IF NOT EXISTS audit(sequence INTEGER PRIMARY KEY AUTOINCREMENT,at TEXT NOT NULL,session_id TEXT NOT NULL,action TEXT NOT NULL);
PRAGMA user_version=1;`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Records{db: db}, nil
}
func (r *Records) begin(a Assignment, c Config) (Session, error) {
	if err := a.validate(); err != nil {
		return Session{}, err
	}
	s := Session{ID: newID(), Assignment: a, StartedAt: time.Now().UTC(), BlenderURL: strings.TrimRight(c.BaseURL, "/"), Revision: 1}
	if c.AerobridgeEnabled {
		if a.AerobridgeOperationID == "" {
			return s, fmt.Errorf("select an Aerobridge flight operation for log routing")
		}
		s.AerobridgeURL = strings.TrimRight(c.AerobridgeURL, "/")
	}
	b, _ := json.Marshal(s)
	tx, err := r.db.Begin()
	if err != nil {
		return s, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO sessions(id,data,system_id) VALUES(?,?,?)", s.ID, string(b), a.SystemID); err != nil {
		return s, fmt.Errorf("cannot start session (a session may already be active for this system): %w", err)
	}
	if _, err = tx.Exec("INSERT INTO audit(at,session_id,action) VALUES(?,?,?)", s.StartedAt.Format(time.RFC3339Nano), s.ID, "session started"); err != nil {
		return s, err
	}
	return s, tx.Commit()
}
func (r *Records) end(id string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRow("SELECT data FROM sessions WHERE id=? AND ended=0", id).Scan(&raw); err != nil {
		return fmt.Errorf("active session not found: %w", err)
	}
	var s Session
	if err = json.Unmarshal([]byte(raw), &s); err != nil {
		return err
	}
	now := time.Now().UTC()
	s.EndedAt = &now
	s.Revision++
	b, _ := json.Marshal(s)
	if _, err = tx.Exec("UPDATE sessions SET ended=1,data=?,revision=revision+1 WHERE id=?", string(b), id); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO audit(at,session_id,action) VALUES(?,?,?)", now.Format(time.RFC3339Nano), id, "session ended"); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Records) append(v Vehicle) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id, metadata string
	if err = tx.QueryRow("SELECT id,data FROM sessions WHERE system_id=? AND ended=0", v.SystemID).Scan(&id, &metadata); err == sql.ErrNoRows {
		return nil
	} else if err != nil {
		return err
	}
	var session Session
	if err = json.Unmarshal([]byte(metadata), &session); err != nil {
		return err
	}
	if v.GPSAt.Before(session.StartedAt) {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO samples(session_id,sequence,data) SELECT ?,COALESCE(MAX(sequence),0)+1,? FROM samples WHERE session_id=?", id, string(b), id); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE sessions SET revision=revision+1 WHERE id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Records) list(limit int) ([]RecordSummary, error) {
	rows, err := r.db.Query(`SELECT data,revision,fb_revision,ab_revision,(SELECT COUNT(*) FROM samples WHERE session_id=sessions.id) FROM sessions ORDER BY rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecordSummary{}
	for rows.Next() {
		var raw string
		var rev, fb, ab, count int64
		if err = rows.Scan(&raw, &rev, &fb, &ab, &count); err != nil {
			return nil, err
		}
		var s Session
		if err = json.Unmarshal([]byte(raw), &s); err != nil {
			return nil, err
		}
		s.Revision = rev
		out = append(out, RecordSummary{s, count, rev > fb, s.AerobridgeURL != "" && rev > ab})
	}
	return out, rows.Err()
}
func (r *Records) batch(id, destination string) (SessionBatch, error) {
	var pending string
	err := r.db.QueryRow("SELECT data FROM deliveries WHERE session_id=? AND destination=?", id, destination).Scan(&pending)
	if err == nil {
		var saved SessionBatch
		err = json.Unmarshal([]byte(pending), &saved)
		return saved, err
	}
	if err != sql.ErrNoRows {
		return SessionBatch{}, err
	}

	prefix := "fb"
	if destination == "aerobridge" {
		prefix = "ab"
	}
	var raw string
	var rev, seq int64
	if err := r.db.QueryRow("SELECT data,revision,"+prefix+"_sequence FROM sessions WHERE id=?", id).Scan(&raw, &rev, &seq); err != nil {
		return SessionBatch{}, err
	}
	var b SessionBatch
	if err := json.Unmarshal([]byte(raw), &b.Session); err != nil {
		return b, err
	}
	b.Session.Revision = rev
	b.Samples = []Sample{}
	rows, err := r.db.Query("SELECT sequence,data FROM samples WHERE session_id=? AND sequence>? ORDER BY sequence LIMIT 200", id, seq)
	if err != nil {
		return b, err
	}
	defer rows.Close()
	for rows.Next() {
		var v Sample
		if err = rows.Scan(&v.Sequence, &raw); err != nil {
			return b, err
		}
		if err = json.Unmarshal([]byte(raw), &v.Vehicle); err != nil {
			return b, err
		}
		b.Samples = append(b.Samples, v)
	}
	if err = rows.Err(); err != nil {
		return b, err
	}
	rows.Close()
	data, err := json.Marshal(b)
	if err != nil {
		return b, err
	}
	_, err = r.db.Exec("INSERT INTO deliveries VALUES(?,?,?)", id, destination, string(data))
	return b, err
}
func (r *Records) ack(b SessionBatch, destination string) error {
	prefix := "fb"
	if destination == "aerobridge" {
		prefix = "ab"
	}
	seq := int64(0)
	if len(b.Samples) > 0 {
		seq = b.Samples[len(b.Samples)-1].Sequence
	}
	// Only acknowledge the metadata revision once all samples at that revision
	// have been delivered. A concurrent append remains pending.
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec("UPDATE sessions SET "+prefix+"_sequence=MAX("+prefix+"_sequence,?),"+prefix+"_revision=CASE WHEN (SELECT COUNT(*) FROM samples WHERE session_id=?)<=MAX("+prefix+"_sequence,?) THEN ? ELSE "+prefix+"_revision END WHERE id=?", seq, b.Session.ID, seq, b.Session.Revision, b.Session.ID)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM deliveries WHERE session_id=? AND destination=?", b.Session.ID, destination); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Records) capture(server, kind, id string, data json.RawMessage) error {
	if !json.Valid(data) || id == "" {
		return fmt.Errorf("invalid %s document", kind)
	}
	hash := sha256.Sum256(data)
	server = strings.TrimRight(server, "/")
	digest := hex.EncodeToString(hash[:])
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous string
	if err := tx.QueryRow("SELECT digest FROM documents WHERE server=? AND kind=? AND id=? ORDER BY rowid DESC LIMIT 1", server, kind, id).Scan(&previous); err == nil && previous == digest && kind != "restrictions" {
		return nil
	}
	// Restriction maps are a current cache; declarations retain every change.
	if kind == "restrictions" {
		if _, err := tx.Exec("DELETE FROM documents WHERE server=? AND kind=? AND id=?", server, kind, id); err != nil {
			return err
		}
	}
	_, err = tx.Exec("INSERT INTO documents(server,kind,id,digest,captured,data) VALUES(?,?,?,?,?,?)", strings.TrimRight(server, "/"), kind, id, hex.EncodeToString(hash[:]), time.Now().UTC().Format(time.RFC3339Nano), string(data))
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Records) documents(server, kind string) ([]Document, error) {
	rows, err := r.db.Query(`SELECT id,captured,data FROM documents WHERE server=? AND kind=? AND rowid IN (SELECT MAX(rowid) FROM documents GROUP BY server,kind,id) ORDER BY id`, strings.TrimRight(server, "/"), kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Document{}
	for rows.Next() {
		d := Document{Kind: kind, Server: server}
		var at, raw string
		if err = rows.Scan(&d.ID, &at, &raw); err != nil {
			return nil, err
		}
		d.CapturedAt, _ = time.Parse(time.RFC3339Nano, at)
		d.Data = json.RawMessage(raw)
		out = append(out, d)
	}
	return out, rows.Err()
}
