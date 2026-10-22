package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/ardupilotmega"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"
	"github.com/bluenviron/gomavlib/v3/pkg/frame"
	"github.com/zalando/go-keyring"
)

type Vehicle struct {
	SystemID         byte      `json:"systemID"`
	LastSeen         time.Time `json:"lastSeen"`
	HeartbeatAt      time.Time `json:"heartbeatAt"`
	PositionAt       time.Time `json:"positionAt"`
	GPSAt            time.Time `json:"gpsAt"`
	Frames           uint64    `json:"frames"`
	Armed            bool      `json:"armed"`
	Latitude         *float64  `json:"latitude"`
	Longitude        *float64  `json:"longitude"`
	AltitudeMSL      *float64  `json:"altitudeMSL"`
	AltitudeWGS84    *float64  `json:"altitudeWGS84"`
	RelativeAltitude *float64  `json:"relativeAltitude"`
	Speed            *float64  `json:"speed"`
	Heading          *float64  `json:"heading"`
	Fix              int       `json:"fix"`
	Satellites       int       `json:"satellites"`
}
type Link struct {
	State     string    `json:"state"`
	Detail    string    `json:"detail"`
	CheckedAt time.Time `json:"checkedAt"`
}
type Activity struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}
type Snapshot struct {
	Config      Config     `json:"config"`
	ConfigPath  string     `json:"configPath"`
	Running     bool       `json:"running"`
	Blender     Link       `json:"blender"`
	Access      Link       `json:"access"`
	Telemetry   Link       `json:"telemetry"`
	Delivery    Link       `json:"delivery"`
	Vehicles    []Vehicle  `json:"vehicles"`
	GCS         []GCS      `json:"gcs"`
	Events      []Activity `json:"events"`
	Frames      uint64     `json:"frames"`
	ParseErrors uint64     `json:"parseErrors"`
	Sent        uint64     `json:"sent"`
	Now         time.Time  `json:"now"`
}
type Service struct {
	op        sync.Mutex
	mu        sync.Mutex
	config    Config
	path      string
	state     Snapshot
	vehicles  map[byte]Vehicle
	node      *gomavlib.Node
	cancel    context.CancelFunc
	done      chan struct{}
	frameDone chan struct{}
}

func NewService(path string) (*Service, error) {
	c, e := LoadConfig(path)
	if e != nil {
		return nil, e
	}
	s := &Service{config: c, path: path, vehicles: map[byte]Vehicle{}}
	s.state.Events = []Activity{}
	s.state.GCS = Discover()
	if raw, e := keyring.Get("org.openutm.adapter", path); e == nil {
		var secrets struct{ Token, ClientSecret string }
		if json.Unmarshal([]byte(raw), &secrets) == nil {
			s.config.Token = secrets.Token
			s.config.ClientSecret = secrets.ClientSecret
		}
	}
	s.resetLinks()
	return s, nil
}
func (s *Service) resetLinks() {
	s.state.Blender = Link{State: "idle", Detail: "Not checked"}
	s.state.Access = Link{State: "idle", Detail: "Not checked"}
	s.state.Telemetry = Link{State: "idle", Detail: "Paused"}
	s.state.Delivery = Link{State: "idle", Detail: "Sending is off"}
}
func (s *Service) event(message string) {
	s.state.Events = append([]Activity{{At: time.Now(), Message: message}}, s.state.Events...)
	if len(s.state.Events) > 40 {
		s.state.Events = s.state.Events[:40]
	}
}
func (s *Service) GetSnapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := s.state
	snap.Now = time.Now()
	snap.Config = s.config
	snap.Config.HasToken = s.config.Token != ""
	snap.Config.HasClientSecret = s.config.ClientSecret != ""
	snap.Config.Token = ""
	snap.Config.ClientSecret = ""
	snap.ConfigPath = s.path
	snap.Events = append([]Activity{}, s.state.Events...)
	snap.GCS = append([]GCS{}, s.state.GCS...)
	snap.Vehicles = []Vehicle{}
	for _, v := range s.vehicles {
		snap.Vehicles = append(snap.Vehicles, v)
	}
	sort.Slice(snap.Vehicles, func(i, j int) bool { return snap.Vehicles[i].SystemID < snap.Vehicles[j].SystemID })
	if snap.Running && snap.Telemetry.State != "error" {
		fresh := false
		for _, v := range snap.Vehicles {
			if time.Since(v.LastSeen) < 5*time.Second {
				fresh = true
			}
		}
		if fresh {
			snap.Telemetry = Link{State: "connected", Detail: "MAVLink frames are parsing"}
		} else {
			snap.Telemetry = Link{State: "waiting", Detail: "Waiting for vehicle telemetry on " + s.config.Address}
		}
	}
	return snap
}
func (s *Service) ApplyConfig(c Config) error {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	old := s.config
	s.mu.Unlock()
	if c.Token == "" {
		c.Token = old.Token
	}
	if c.ClientSecret == "" {
		c.ClientSecret = old.ClientSecret
	}
	if e := c.Validate(); e != nil {
		return e
	}
	if c.Token != old.Token || c.ClientSecret != old.ClientSecret {
		raw, _ := json.Marshal(struct{ Token, ClientSecret string }{c.Token, c.ClientSecret})
		if e := keyring.Set("org.openutm.adapter", s.path, string(raw)); e != nil {
			return fmt.Errorf("cannot save credentials to the OS credential store: %w", e)
		}
	}
	if e := SaveConfig(s.path, c); e != nil {
		return e
	}
	s.stop()
	s.mu.Lock()
	s.config = c
	s.mu.Unlock()
	return s.start()
}
func (s *Service) ClearCredentials() error {
	s.op.Lock()
	defer s.op.Unlock()
	if e := keyring.Delete("org.openutm.adapter", s.path); e != nil && e != keyring.ErrNotFound {
		return e
	}
	s.stop()
	s.mu.Lock()
	s.config.Token = ""
	s.config.ClientSecret = ""
	s.config.Publish = false
	c := s.config
	s.event("Credentials removed; telemetry sending disabled")
	s.mu.Unlock()
	return SaveConfig(s.path, c)
}
func (s *Service) Start() error { s.op.Lock(); defer s.op.Unlock(); s.stop(); return s.start() }
func (s *Service) Stop()        { s.op.Lock(); defer s.op.Unlock(); s.stop() }
func (s *Service) stop() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.node != nil {
		s.node.Close()
		s.node = nil
	}
	if s.done != nil {
		<-s.done
		s.done = nil
	}
	if s.frameDone != nil {
		<-s.frameDone
		s.frameDone = nil
	}
	s.mu.Lock()
	s.state.Running = false
	s.resetLinks()
	s.mu.Unlock()
}
func (s *Service) start() error {
	s.mu.Lock()
	c := s.config
	s.mu.Unlock()
	if e := c.Validate(); e != nil {
		return e
	}
	var ep gomavlib.EndpointConf
	switch c.Transport {
	case "udp":
		ep = gomavlib.EndpointUDPServer{Address: c.Address}
	case "tcp":
		ep = gomavlib.EndpointTCPClient{Address: c.Address}
	case "serial":
		ep = gomavlib.EndpointSerial{Device: c.SerialDevice, Baud: c.Baud}
	}
	node := &gomavlib.Node{Endpoints: []gomavlib.EndpointConf{ep}, Dialect: ardupilotmega.Dialect, OutVersion: gomavlib.V2, OutSystemID: 254, HeartbeatDisable: true, StreamRequestEnable: false}
	if e := node.Initialize(); e != nil {
		s.mu.Lock()
		s.state.Telemetry = Link{State: "error", Detail: e.Error()}
		s.mu.Unlock()
		return e
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.node = node
	s.done = make(chan struct{})
	s.frameDone = make(chan struct{})
	s.mu.Lock()
	s.state.Running = true
	s.vehicles = map[byte]Vehicle{}
	s.state.Frames = 0
	s.state.ParseErrors = 0
	s.state.Sent = 0
	s.state.Telemetry = Link{State: "waiting", Detail: "Listening for MAVLink"}
	s.event("Adapter started — " + c.Transport + " " + c.Address)
	s.mu.Unlock()
	go func() {
		defer close(s.frameDone)
		for ev := range node.Events() {
			s.consume(ev)
		}
	}()
	go s.run(ctx, c, s.done)
	return nil
}
func f64(v float64) *float64 { return &v }
func (s *Service) consume(ev gomavlib.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch e := ev.(type) {
	case *gomavlib.EventParseError:
		s.state.ParseErrors++
	case *gomavlib.EventChannelClose:
		s.event("MAVLink channel closed; waiting for reconnection")
	case *gomavlib.EventFrame:
		s.state.Frames++
		if h, ok := e.Message().(*common.MessageHeartbeat); ok && h.Type == common.MAV_TYPE_GCS {
			return
		}
		if e.ComponentID() != 1 {
			return
		}
		id := e.SystemID()
		if s.config.SystemID != 0 && int(id) != s.config.SystemID {
			return
		}
		v, exists := s.vehicles[id]
		v.SystemID = id
		v.LastSeen = time.Now()
		v.Frames++
		switch m := e.Message().(type) {
		case *common.MessageHeartbeat:
			v.Armed = m.BaseMode&common.MAV_MODE_FLAG_SAFETY_ARMED != 0
			v.HeartbeatAt = time.Now()
		case *common.MessageGlobalPositionInt:
			// Keep GPS position, WGS84 altitude and sample timestamp together.
			if v.GPSAt.IsZero() {
				v.Latitude = f64(float64(m.Lat) / 1e7)
				v.Longitude = f64(float64(m.Lon) / 1e7)
				v.PositionAt = time.Now()
			}
			v.AltitudeMSL = f64(float64(m.Alt) / 1000)
			v.RelativeAltitude = f64(float64(m.RelativeAlt) / 1000)
		case *common.MessageGpsRawInt:
			v.Fix = int(m.FixType)
			v.Satellites = int(m.SatellitesVisible)
			v.GPSAt = time.Now()
			v.AltitudeWGS84 = nil
			v.Speed = nil
			v.Heading = nil
			if v.Fix >= 3 && v.Fix <= 8 {
				v.Latitude = f64(float64(m.Lat) / 1e7)
				v.Longitude = f64(float64(m.Lon) / 1e7)
				v.AltitudeMSL = f64(float64(m.Alt) / 1000)
				v.PositionAt = time.Now()
				if m.Vel != 65535 {
					v.Speed = f64(float64(m.Vel) / 100)
				}
				if m.Cog < 36000 {
					v.Heading = f64(float64(m.Cog) / 100)
				}
				if _, ok := e.Frame.(*frame.V2Frame); ok && m.AltEllipsoid != 0 {
					v.AltitudeWGS84 = f64(float64(m.AltEllipsoid) / 1000)
				}
			}
		}
		s.vehicles[id] = v
		if !exists {
			s.event(fmt.Sprintf("Vehicle detected · MAVLink system %d", id))
		}
	}
}
func (s *Service) run(ctx context.Context, c Config, done chan struct{}) {
	defer close(done)
	b := NewBlender(c)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	checks := 0
	lastSent := time.Time{}
	for {
		if ctx.Err() != nil {
			return
		}
		if checks%10 == 0 {
			found := configureGCS(c, Discover())
			health := b.Health(ctx)
			var access error
			if health == nil {
				access = b.CheckAccess(ctx)
			}
			s.mu.Lock()
			for i := range found {
				g := &found[i]
				for _, old := range s.state.GCS {
					if g.ID == old.ID && g.BackupPath == "" {
						g.BackupPath = old.BackupPath
					}
					if g.ID == old.ID && g.SetupDetail != old.SetupDetail {
						s.event(g.Name + ": " + g.SetupDetail)
					}
				}
			}
			s.state.GCS = found
			s.state.Blender = linkResult(health, "Flight Blender is reachable")
			if health == nil {
				s.state.Access = linkResult(access, "Flight Blender API access verified")
			} else {
				s.state.Access = Link{State: "waiting", Detail: "Waiting for Flight Blender"}
			}
			s.mu.Unlock()
		}
		if c.Publish {
			s.mu.Lock()
			v, ok := s.vehicles[byte(c.SystemID)]
			s.mu.Unlock()
			var err error
			if !ok || time.Since(v.GPSAt) > 5*time.Second || time.Since(v.HeartbeatAt) > 5*time.Second {
				err = fmt.Errorf("waiting for fresh GPS and vehicle heartbeat")
			} else if !v.GPSAt.After(lastSent) {
				err = fmt.Errorf("waiting for a new GPS sample")
			} else {
				err = b.Publish(ctx, v)
				if err == nil {
					lastSent = v.GPSAt
					s.mu.Lock()
					s.state.Sent++
					s.mu.Unlock()
				}
			}
			s.mu.Lock()
			s.state.Delivery = linkResult(err, "Telemetry accepted by Flight Blender")
			s.mu.Unlock()
		}
		checks++
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func linkResult(err error, ok string) Link {
	if err != nil {
		return Link{State: "error", Detail: err.Error(), CheckedAt: time.Now()}
	}
	return Link{State: "connected", Detail: ok, CheckedAt: time.Now()}
}
func ConfigPath() string {
	if p := os.Getenv("UTM_ADAPTER_CONFIG"); p != "" {
		return p
	}
	root, e := os.UserConfigDir()
	if e != nil {
		root = "."
	}
	return filepath.Join(root, "OpenUTM", "utm-adapter", "product.json")
}

// Removal is explicit; clearing the opt-in alone never removes installed files.
func (s *Service) RemoveMissionPlannerPlugin() (result error) {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	c := s.config
	running := s.state.Running
	s.mu.Unlock()
	s.stop()
	defer func() {
		if running {
			result = errors.Join(result, s.start())
		}
	}()
	// Persist the opt-out before touching the plugin so it cannot be reinstalled.
	c.MissionPlannerPlugin = false
	if err := SaveConfig(s.path, c); err != nil {
		return err
	}
	s.mu.Lock()
	s.config = c
	s.mu.Unlock()
	found := Discover()
	for _, g := range found {
		if g.ID == "mission-planner" {
			backup, err := removeMissionPlannerPlugin(g)
			if err != nil {
				return err
			}
			found = configureGCS(c, found)
			s.mu.Lock()
			s.event("Mission Planner plugin removed; automatic installation disabled")
			if backup != "" {
				s.event("Plugin backup: " + backup)
			}
			s.state.GCS = found
			s.mu.Unlock()
			return nil
		}
	}
	return fmt.Errorf("Mission Planner installation could not be located")
}
