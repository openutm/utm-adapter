package adapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func assignment(system int) Assignment {
	return Assignment{SystemID: system, DeclarationID: "00000000-0000-0000-0000-000000000001", VehicleID: "vehicle-1", OperatorID: "operator-1", Pilot: Pilot{Name: "Test Pilot", License: "TEST-LICENCE", Contact: "pilot@example.test"}}
}
func TestRecordsRestartAndCursors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.sqlite")
	r, err := openRecords(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.begin(assignment(1), DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.begin(assignment(1), DefaultConfig()); err == nil {
		t.Fatal("duplicate active system allowed")
	}
	other, err := r.begin(assignment(2), DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	// A GPS event queued before Start must not enter the new session.
	if err = r.append(Vehicle{SystemID: 1, GPSAt: s.StartedAt.Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 205; i++ {
		if err = r.append(Vehicle{SystemID: 1, GPSAt: time.Now().UTC(), Speed: f64(float64(i))}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := r.batch(s.ID, "blender")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Samples) != 200 {
		t.Fatalf("batch size %d", len(first.Samples))
	}
	// More telemetry and a restart after a potentially successful remote POST
	// must not change the pending batch or Aerobridge's deduplication key.
	if err = r.append(Vehicle{SystemID: 1, GPSAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err = r.db.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = openRecords(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.db.Close()
	again, err := r.batch(s.ID, "blender")
	if err != nil {
		t.Fatal(err)
	}
	one, _ := json.Marshal(first)
	two, _ := json.Marshal(again)
	if string(one) != string(two) {
		t.Fatal("pending batch changed across restart")
	}
	if err = r.ack(again, "blender"); err != nil {
		t.Fatal(err)
	}
	next, err := r.batch(s.ID, "blender")
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Samples) != 6 || next.Samples[0].Sequence != 201 {
		t.Fatalf("incorrect next batch: %+v", next.Samples)
	}
	if err = r.ack(next, "blender"); err != nil {
		t.Fatal(err)
	}
	if err = r.end(s.ID); err != nil {
		t.Fatal(err)
	}
	last, err := r.batch(s.ID, "blender")
	if err != nil {
		t.Fatal(err)
	}
	if last.Session.EndedAt == nil || len(last.Samples) != 0 {
		t.Fatal("end metadata missing")
	}
	if err = r.ack(last, "blender"); err != nil {
		t.Fatal(err)
	}
	list, err := r.list(10)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list {
		if item.Session.ID == s.ID && (item.BlenderPending || item.Samples != 206) {
			t.Fatalf("wrong acknowledgement %+v", item)
		}
		if item.Session.ID == other.ID && item.Samples != 0 {
			t.Fatal("telemetry crossed vehicle assignments")
		}
	}
	if err = r.append(Vehicle{SystemID: 1}); err != nil {
		t.Fatal(err)
	} // ended flights are immutable
}
func TestDocumentHistoryAndExport(t *testing.T) {
	c := DefaultConfig()
	s := testService(t, c)
	if err := s.ensureRecords(); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{`{"id":"d","state":1}`, `{"id":"d","state":1}`, `{"id":"d","state":2}`, `{"id":"d","state":1}`} {
		if err := s.records.capture(c.BaseURL, "declaration", "d", json.RawMessage(data)); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := s.records.db.QueryRow("SELECT COUNT(*) FROM documents").Scan(&count); err != nil || count != 3 {
		t.Fatalf("history=%d err=%v", count, err)
	}
	docs, err := s.GetDeclarations()
	if err != nil || len(docs) != 1 || string(docs[0].Data) != `{"id":"d","state":1}` {
		t.Fatalf("cache %+v %v", docs, err)
	}
	flight, err := s.BeginSession(assignment(1))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.records.append(Vehicle{SystemID: 1, GPSAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err = s.EndSession(flight.ID); err != nil {
		t.Fatal(err)
	}
	path, err := s.ExportSession(flight.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Session Session
		Samples []Sample
		Audit   []map[string]string
	}
	if err = json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Samples) != 1 || len(report.Audit) != 2 || report.Session.Assignment.Pilot.Name != "Test Pilot" {
		t.Fatalf("incomplete report %s", raw)
	}
	if _, err = s.ExportSession("../escape"); err == nil {
		t.Fatal("invalid export accepted")
	}
}
