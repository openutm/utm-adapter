package adapter

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"
)

// GCSIntegration adds a compiled-in ground station without changing transport,
// session storage or delivery. Implementations must be safe for concurrent calls.
// Discover must honor cancellation. Configure must preserve unrelated settings,
// back up changed files and never change files while the station is running.
type GCSIntegration interface {
	ID() string
	Discover(context.Context) GCS
	Configure(Config, GCS) GCS
}

var gcsRegistry = struct {
	sync.RWMutex
	items []GCSIntegration
}{items: []GCSIntegration{builtinGCS("qgroundcontrol"), builtinGCS("mission-planner")}}

var gcsID = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// RegisterGCSIntegration is intended for application startup (for example init).
// IDs are stable product.json source values; existing IDs cannot be replaced.
func RegisterGCSIntegration(integration GCSIntegration) error {
	if integration == nil {
		return fmt.Errorf("GCS integration is required")
	}
	id := integration.ID()
	if !gcsID.MatchString(id) || id == "auto" || id == "direct" {
		return fmt.Errorf("invalid or reserved GCS ID %q", id)
	}
	gcsRegistry.Lock()
	defer gcsRegistry.Unlock()
	for _, existing := range gcsRegistry.items {
		if existing.ID() == id {
			return fmt.Errorf("GCS ID %q is already registered", id)
		}
	}
	gcsRegistry.items = append(gcsRegistry.items, integration)
	return nil
}

func gcsIntegrations() []GCSIntegration {
	gcsRegistry.RLock()
	defer gcsRegistry.RUnlock()
	return append([]GCSIntegration{}, gcsRegistry.items...)
}

func knownGCS(id string) bool {
	for _, integration := range gcsIntegrations() {
		if integration.ID() == id {
			return true
		}
	}
	return false
}

func Discover() []GCS {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := []GCS{}
	for _, integration := range gcsIntegrations() {
		g := integration.Discover(ctx)
		g.ID = integration.ID()
		result = append(result, g)
	}
	return result
}

func configureGCS(c Config, found []GCS) []GCS {
	integrations := map[string]GCSIntegration{}
	for _, integration := range gcsIntegrations() {
		integrations[integration.ID()] = integration
	}
	for i, g := range found {
		integration, ok := integrations[g.ID]
		if !ok {
			found[i].SetupState, found[i].SetupDetail = "attention", "No registered integration for this ground station"
			continue
		}
		// Every integration gets the same opt-out and source-selection rules.
		// Builtins also inspect their optional enhancement's on-disk state.
		if _, builtin := integration.(builtinGCS); !builtin {
			if !g.Installed {
				continue
			}
			if !c.AutoConfigureGCS || c.Source == "direct" || (c.Source != "auto" && c.Source != g.ID) {
				found[i].SetupState, found[i].SetupDetail = "manual", "Automatic setup is off for this ground station"
				continue
			}
			if _, err := forwardingTarget(c); err != nil {
				found[i].SetupState, found[i].SetupDetail = "attention", err.Error()
				continue
			}
			if g.Running {
				found[i].SetupState, found[i].SetupDetail = "close", "Close "+g.Name+" before configuring forwarding"
				continue
			}
		}
		found[i] = integration.Configure(c, g)
		found[i].ID = g.ID
	}
	return found
}

type builtinGCS string

func (b builtinGCS) ID() string                       { return string(b) }
func (b builtinGCS) Discover(ctx context.Context) GCS { return discoverBuiltin(ctx, b.ID()) }
func (b builtinGCS) Configure(c Config, g GCS) GCS {
	return configureBuiltinGCS(c, []GCS{g})[0]
}
