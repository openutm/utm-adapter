package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

const requestBudget = 45 * time.Second

type RestrictionMap struct {
	Type      string            `json:"type"`
	Features  []json.RawMessage `json:"features"`
	FetchedAt time.Time         `json:"fetchedAt"`
	Stale     bool              `json:"stale"`
}
type localBridge struct {
	server *http.Server
	url    string
	done   chan struct{}
}

func localTarget(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("use a loopback IP address")
	}
	return net.JoinHostPort(host, port), nil
}
func (s *Service) startBridge(address string) (*localBridge, error) {
	if address == "" {
		return nil, nil
	}
	if _, err := localTarget(address); err != nil {
		return nil, err
	}
	l, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("GCS restrictions bridge: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/restrictions", func(w http.ResponseWriter, r *http.Request) {
		// Do not expose credentials or pilot information on the local map feed.
		s.mu.Lock()
		base := s.config.BaseURL
		records := s.records
		s.mu.Unlock()
		if records == nil {
			http.Error(w, "restriction cache unavailable", 503)
			return
		}
		docs, e := records.documents(base, "restrictions")
		if e != nil {
			http.Error(w, "restriction cache unavailable", 503)
			return
		}
		m := RestrictionMap{Type: "FeatureCollection", Features: []json.RawMessage{}, Stale: true}
		if len(docs) > 0 {
			if json.Unmarshal(docs[0].Data, &m) != nil {
				http.Error(w, "invalid restriction cache", 503)
				return
			}
			m.FetchedAt = docs[0].CapturedAt
			m.Stale = time.Since(m.FetchedAt) > 2*time.Minute
		}
		w.Header().Set("Content-Type", "application/geo+json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(m)
	})
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": Version})
	})
	b := &localBridge{server: &http.Server{Handler: mux, ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}, url: "http://" + l.Addr().String(), done: make(chan struct{})}
	go func() { defer close(b.done); _ = b.server.Serve(l) }()
	return b, nil
}
func (b *localBridge) close() { _ = b.server.Close(); <-b.done }

func (s *Service) refreshRestrictions(ctx context.Context, b *Blender) error {
	m := RestrictionMap{Type: "FeatureCollection", Features: []json.RawMessage{}}
	for offset := 0; offset < 1000000; offset += 100 {
		raw, err := b.request(ctx, "GET", fmt.Sprintf("/geo_fence_ops/geo_fence?limit=100&offset=%d", offset), nil, true)
		if err != nil {
			return err
		}
		var p apiPage
		if err = json.Unmarshal(raw, &p); err != nil || p.Results == nil {
			return fmt.Errorf("invalid restrictions page")
		}
		for _, rawFence := range p.Results {
			var fence struct {
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Raw   json.RawMessage `json:"raw_geo_fence"`
				Start string          `json:"start_datetime"`
				End   string          `json:"end_datetime"`
			}
			if err = json.Unmarshal(rawFence, &fence); err != nil {
				return err
			}
			geo := fence.Raw
			if len(geo) > 0 && geo[0] == '"' {
				var str string
				if err = json.Unmarshal(geo, &str); err != nil {
					return err
				}
				geo = json.RawMessage(str)
			}
			var fc struct {
				Type     string           `json:"type"`
				Features []map[string]any `json:"features"`
			}
			if err = json.Unmarshal(geo, &fc); err != nil || fc.Type != "FeatureCollection" {
				return fmt.Errorf("restriction %s has invalid GeoJSON", fence.ID)
			}
			for _, feature := range fc.Features {
				props, ok := feature["properties"].(map[string]any)
				if !ok {
					props = map[string]any{}
				}
				props["restrictionID"] = fence.ID
				props["name"] = fence.Name
				props["start_datetime"] = fence.Start
				props["end_datetime"] = fence.End
				feature["properties"] = props
				data, err := json.Marshal(feature)
				if err != nil {
					return err
				}
				m.Features = append(m.Features, data)
			}
		}
		// Geofence count currently describes this page, not the total. Continue
		// until a short page rather than trusting count or an absent next field.
		if len(p.Results) < 100 {
			m.FetchedAt = time.Now().UTC()
			raw, err = json.Marshal(m)
			if err != nil {
				return err
			}
			return s.records.capture(strings.TrimRight(b.config.BaseURL, "/"), "restrictions", "current", raw)
		}
	}
	return fmt.Errorf("restrictions pagination limit exceeded")
}
