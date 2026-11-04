package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPopulatedFleetSelectionAndLogRouting(t *testing.T) {
	ids := []string{"10000000-0000-0000-0000-000000000001", "10000000-0000-0000-0000-000000000002"}
	pilotIDs := []string{"20000000-0000-0000-0000-000000000001", "20000000-0000-0000-0000-000000000002"}
	operations := []string{"30000000-0000-0000-0000-000000000001", "30000000-0000-0000-0000-000000000002"}
	logs := map[string]SessionBatch{}
	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fleet-secret" {
			http.Error(w, "unauthorized", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/registry/aircraft/":
			pages++
			if r.URL.Query().Get("page") == "1" {
				// An untrusted next link must only signal another local page; never be followed.
				fmt.Fprintf(w, `{"count":2,"next":"https://untrusted.invalid/steal-token","results":[{"id":%q,"name":"Aircraft One"}]}`, ids[0])
			} else {
				fmt.Fprintf(w, `{"count":2,"next":null,"results":[{"id":%q,"name":"Aircraft Two"}]}`, ids[1])
			}
		case "/registry/pilots/":
			fmt.Fprintf(w, `[{"id":%q,"name":"Pilot One"},{"id":%q,"name":"Pilot Two"}]`, pilotIDs[0], pilotIDs[1])
		case "/gcs/flight-operations":
			fmt.Fprintf(w, `{"count":2,"next":null,"results":[{"id":%q},{"id":%q}]}`, operations[0], operations[1])
		case "/gcs/flight-logs":
			if r.Method == "GET" {
				if r.URL.Query().Get("operation") == "" {
					t.Error("missing operation filter")
				}
				fmt.Fprint(w, `[]`)
				return
			}
			var log struct {
				Operation string `json:"operation"`
				Raw       struct {
					Batch SessionBatch `json:"batch"`
				} `json:"raw_log"`
			}
			if err := json.NewDecoder(r.Body).Decode(&log); err != nil {
				t.Error(err)
				http.Error(w, "bad log", 400)
				return
			}
			logs[log.Operation] = log.Raw.Batch
			w.WriteHeader(201)
			fmt.Fprint(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := DefaultConfig()
	c.AerobridgeEnabled = true
	c.AerobridgeURL = server.URL
	c.AerobridgeToken = "fleet-secret"
	s := testService(t, c)
	defer s.Close()
	f, err := s.GetFleet()
	if err != nil {
		t.Fatal(err)
	}
	if pages != 2 || len(f.Aircraft) != 2 || len(f.Pilots) != 2 || len(f.Operations) != 2 {
		t.Fatalf("incomplete registry: %+v (%d pages)", f, pages)
	}
	for i := range ids {
		a := assignment(i + 1)
		a.VehicleID = f.Aircraft[i].ID
		a.Pilot.ID = f.Pilots[i].ID
		a.Pilot.Name = fmt.Sprintf("Pilot %d", i+1)
		a.AerobridgeOperationID = f.Operations[i].ID
		session, err := s.BeginSession(a)
		if err != nil {
			t.Fatal(err)
		}
		// Subsequent UI edits must not change the immutable assignment.
		a.VehicleID = "edited"
		a.Pilot.Name = "edited"
		if err := s.EndSession(session.ID); err != nil {
			t.Fatal(err)
		}
		batch, err := s.records.batch(session.ID, "aerobridge")
		if err != nil {
			t.Fatal(err)
		}
		if err := NewAerobridge(c).Log(context.Background(), batch); err != nil {
			t.Fatal(err)
		}
	}
	for i, operation := range operations {
		batch, ok := logs[operation]
		if !ok || batch.Session.Assignment.VehicleID != ids[i] || batch.Session.Assignment.Pilot.ID != pilotIDs[i] || batch.Session.Assignment.Pilot.Name != fmt.Sprintf("Pilot %d", i+1) {
			t.Fatalf("wrong routed assignment for %s: %+v", operation, batch)
		}
	}
	s.config.AerobridgeToken = "bad-secret"
	if _, err := s.GetFleet(); err == nil {
		t.Fatal("registry authentication failure ignored")
	}
	s.config.AerobridgeEnabled = false
	if _, err := s.GetFleet(); err == nil {
		t.Fatal("disabled registry queried")
	}
}
