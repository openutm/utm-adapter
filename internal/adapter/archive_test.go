package adapter

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestSharedArchiveAcrossBatchesAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.sqlite")
	r, err := openRecords(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.begin(assignment(1), DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 205; i++ {
		if err := r.append(Vehicle{SystemID: 1, GPSAt: time.Now().UTC(), Latitude: f64(47), Longitude: f64(19), Speed: f64(float64(i))}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.end(s.ID); err != nil {
		t.Fatal(err)
	}
	first, err := r.batch(s.ID, "blender")
	if err != nil {
		t.Fatal(err)
	}
	wire, err := r.archive(first)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(wire)
	if len(wire.Samples) != 200 || wire.Samples[0].Sequence != 0 || *wire.Session.FinalSampleCount != 205 {
		t.Fatal("incorrect archive boundaries")
	}
	r.db.Close()
	r, err = openRecords(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.db.Close()
	retry, err := r.batch(s.ID, "blender")
	if err != nil {
		t.Fatal(err)
	}
	again, err := r.archive(retry)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(again)
	if string(before) != string(after) {
		t.Fatal("restart changed archive retry bytes")
	}
	if err := r.ack(first, "blender"); err != nil {
		t.Fatal(err)
	}
	last, err := r.batch(s.ID, "blender")
	if err != nil {
		t.Fatal(err)
	}
	final, err := r.archive(last)
	if err != nil {
		t.Fatal(err)
	}
	if len(final.Samples) != 5 || final.Samples[0].Sequence != 200 || final.Samples[4].Sequence != 204 {
		t.Fatal("archive lost a sample boundary")
	}
	for _, invalid := range []string{`{}`, `{"id":"other","archive":{"complete":true}}`, `{"id":"` + s.ID + `","archive":{"complete":false,"receivedSampleCount":204,"finalSampleCount":205}}`} {
		if validateArchiveReceipt([]byte(invalid), final) == nil {
			t.Fatalf("accepted incomplete acknowledgement: %s", invalid)
		}
	}
	receipt := `{"id":"` + s.ID + `","archive":{"complete":true,"receivedSampleCount":205,"finalSampleCount":205}}`
	if err := validateArchiveReceipt([]byte(receipt), final); err != nil {
		t.Fatal(err)
	}
	if err := r.ack(last, "blender"); err != nil {
		t.Fatal(err)
	}
	pending, err := r.list(10)
	if err != nil || len(pending) != 1 || pending[0].BlenderPending {
		t.Fatalf("archive remains pending: %+v %v", pending, err)
	}
}

func TestSharedArchiveTimestampPrecision(t *testing.T) {
	r, err := openRecords(filepath.Join(t.TempDir(), "records.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.db.Close()
	s, err := r.begin(assignment(1), DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 25, 12, 0, 0, 123456789, time.FixedZone("offset", 7200))
	ended := at.Add(time.Second)
	s.StartedAt, s.EndedAt = at.Add(-time.Second), &ended
	v := Vehicle{GPSAt: at, Latitude: f64(47), Longitude: f64(19)}
	wire, err := r.archive(SessionBatch{Session: s, Samples: []Sample{{Sequence: 1, Vehicle: v}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, stamp := range []time.Time{wire.Session.StartedAt, *wire.Session.EndedAt, wire.Samples[0].RecordedAt} {
		if stamp.Nanosecond()%1000 != 0 || stamp.Location() != time.UTC {
			t.Fatalf("noncanonical timestamp: %v", stamp)
		}
	}
	if !wire.Samples[0].RecordedAt.Equal(at.Truncate(time.Microsecond)) || wire.Samples[0].SourceData.GPSAt != at {
		t.Fatal("transport precision or original source evidence changed")
	}
}
