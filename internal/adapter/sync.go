package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

type apiPage struct {
	Count   int               `json:"count"`
	Results []json.RawMessage `json:"results"`
	Next    *string           `json:"next"`
}

func (b *Blender) captureDocuments(ctx context.Context, r *Records) error {
	// Flight Blender defaults to a two-day window. Explicit dates are required
	// to archive historical declarations, and count (not next) drives paging.
	for page := 1; page <= 10000; page++ {
		raw, err := b.request(ctx, "GET", fmt.Sprintf("/flight_declaration_ops/flight_declaration?start_date=1970-01-01&end_date=9999-12-31&page_size=100&page=%d", page), nil, true)
		if err != nil {
			return err
		}
		var p apiPage
		if err = json.Unmarshal(raw, &p); err != nil || p.Results == nil {
			return fmt.Errorf("invalid declaration page")
		}
		for _, d := range p.Results {
			var id struct {
				ID string `json:"id"`
			}
			if err = json.Unmarshal(d, &id); err != nil {
				return err
			}
			if err = r.capture(b.config.BaseURL, "declaration", id.ID, d); err != nil {
				return err
			}
		}
		if page*100 >= p.Count {
			return nil
		}
		if len(p.Results) == 0 {
			return fmt.Errorf("incomplete declaration pagination")
		}
	}
	return fmt.Errorf("declaration pagination limit exceeded")
}

type RegistryEntity struct {
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data"`
}
type Fleet struct {
	Aircraft   []RegistryEntity `json:"aircraft"`
	Pilots     []RegistryEntity `json:"pilots"`
	Operations []RegistryEntity `json:"operations"`
}
type Aerobridge struct {
	client  *Blender
	enabled bool
}

func NewAerobridge(c Config) *Aerobridge {
	return &Aerobridge{client: NewBlender(Config{BaseURL: c.AerobridgeURL, AuthMode: "bearer", Token: c.AerobridgeToken}), enabled: c.AerobridgeEnabled}
}
func (a *Aerobridge) list(ctx context.Context, path string) ([]RegistryEntity, error) {
	out := []RegistryEntity{}
	for page := 1; page <= 10000; page++ {
		// Never follow an API-supplied URL with the registry bearer credential.
		sep := "?"
		if len(path) > 0 && containsQuery(path) {
			sep = "&"
		}
		raw, err := a.client.request(ctx, "GET", fmt.Sprintf("%s%spage=%d", path, sep, page), nil, true)
		if err != nil {
			return nil, fmt.Errorf("Aerobridge: %w", err)
		}
		var items []json.RawMessage
		var p apiPage
		if err = json.Unmarshal(raw, &items); err != nil {
			if err = json.Unmarshal(raw, &p); err != nil || p.Results == nil {
				return nil, fmt.Errorf("invalid Aerobridge collection")
			}
			items = p.Results
		}
		for _, d := range items {
			var item RegistryEntity
			var id struct {
				ID string `json:"id"`
			}
			if err = json.Unmarshal(d, &id); err != nil || !validUUID(id.ID) {
				return nil, fmt.Errorf("invalid Aerobridge entity ID")
			}
			item.ID = id.ID
			item.Data = d
			out = append(out, item)
		}
		if p.Next == nil || *p.Next == "" {
			return out, nil
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("incomplete Aerobridge pagination")
		}
	}
	return nil, fmt.Errorf("Aerobridge pagination limit exceeded")
}
func containsQuery(s string) bool { u, _ := url.Parse(s); return u != nil && u.RawQuery != "" }
func (a *Aerobridge) Health(ctx context.Context) error {
	_, err := a.list(ctx, "/registry/aircraft/")
	return err
}
func (a *Aerobridge) Fleet(ctx context.Context) (Fleet, error) {
	f := Fleet{}
	var err error
	if f.Aircraft, err = a.list(ctx, "/registry/aircraft/"); err != nil {
		return f, err
	}
	if f.Pilots, err = a.list(ctx, "/registry/pilots/"); err != nil {
		return f, err
	}
	f.Operations, err = a.list(ctx, "/gcs/flight-operations")
	return f, err
}
func (a *Aerobridge) Log(ctx context.Context, b SessionBatch) error {
	// Aerobridge POST has no idempotency key support. Reconcile a stable batch
	// key before each POST, including after an ambiguous network failure.
	end := int64(0)
	if len(b.Samples) > 0 {
		end = b.Samples[len(b.Samples)-1].Sequence
	}
	key := fmt.Sprintf("%s:%d:%d", b.Session.ID, b.Session.Revision, end)
	logs, err := a.list(ctx, "/gcs/flight-logs?operation="+url.QueryEscape(b.Session.Assignment.AerobridgeOperationID))
	if err != nil {
		return err
	}
	for _, entry := range logs {
		var log struct {
			Operation string `json:"operation"`
			Raw       struct {
				Key string `json:"adapterBatchKey"`
			} `json:"raw_log"`
		}
		if err = json.Unmarshal(entry.Data, &log); err != nil {
			return err
		}
		if log.Operation == b.Session.Assignment.AerobridgeOperationID && log.Raw.Key == key {
			return nil
		}
	}
	_, err = a.client.request(ctx, "POST", "/gcs/flight-logs", map[string]any{"operation": b.Session.Assignment.AerobridgeOperationID, "raw_log": map[string]any{"adapterBatchKey": key, "schemaVersion": 1, "batch": b}}, true)
	return err
}
func (s *Service) GetFleet() (Fleet, error) {
	s.mu.Lock()
	c := s.config
	s.mu.Unlock()
	if !c.AerobridgeEnabled {
		return Fleet{}, fmt.Errorf("enable Aerobridge first")
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestBudget)
	defer cancel()
	return NewAerobridge(c).Fleet(ctx)
}
