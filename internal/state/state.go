package state

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"vpp-lab/internal/model"
)

type Store struct {
	mu        sync.RWMutex
	devices   map[string]model.Device
	telemetry map[string]model.Telemetry
	commands  []model.CommandRecord
	events    []model.DeviceEvent
	redis     *redis.Client
	siteID    string
	redisKey  string
}

func NewStore() *Store {
	return &Store{
		devices:   make(map[string]model.Device),
		telemetry: make(map[string]model.Telemetry),
		commands:  make([]model.CommandRecord, 0, 200),
		events:    make([]model.DeviceEvent, 0, 200),
	}
}

func NewRedisStore(ctx context.Context, siteID string, opts RedisOptions) (*Store, error) {
	store := NewStore()
	client := redis.NewClient(&redis.Options{
		Addr:         opts.Addr,
		Password:     opts.Password,
		DB:           opts.DB,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	store.redis = client
	store.siteID = siteID
	store.redisKey = telemetryKey(siteID)
	if err := store.loadTelemetry(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return store, nil
}

type RedisOptions struct {
	Addr     string
	Password string
	DB       int
}

func (s *Store) Close() error {
	if s.redis == nil {
		return nil
	}
	return s.redis.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	if s.redis == nil {
		return nil
	}
	return s.redis.Ping(ctx).Err()
}

func (s *Store) UpsertDevice(d model.Device) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now()
	}
	s.devices[stateKey(d.SiteID, d.ID)] = d
}

func (s *Store) Devices() []model.Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Device, 0, len(s.devices))
	for _, d := range s.devices {
		out = append(out, d)
	}
	return out
}

func (s *Store) Device(id string) (model.Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.deviceByIDLocked(id)
	return d, ok
}

func (s *Store) DeviceInSite(siteID, id string) (model.Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.devices[stateKey(siteID, id)]
	return d, ok
}

func (s *Store) PutTelemetry(t model.Telemetry) {
	s.mu.Lock()
	if t.SiteID == "" {
		if d, ok := s.deviceByIDLocked(t.DeviceID); ok {
			t.SiteID = d.SiteID
		} else if s.siteID != "" {
			t.SiteID = s.siteID
		}
	}
	s.telemetry[stateKey(t.SiteID, t.DeviceID)] = t
	s.mu.Unlock()
	s.persistTelemetry(t)
}

func (s *Store) Telemetry() map[string]model.Telemetry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]model.Telemetry, len(s.telemetry))
	for k, v := range s.telemetry {
		out[k] = v
	}
	return out
}

func (s *Store) TelemetryForSite(siteID string) map[string]model.Telemetry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]model.Telemetry)
	for _, t := range s.telemetry {
		if t.SiteID == siteID {
			out[t.DeviceID] = t
		}
	}
	return out
}

func (s *Store) Summary(siteID string) model.SiteSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var summary model.SiteSummary
	summary.SiteID = siteID
	var socSum float64
	var socCount int
	for _, t := range s.telemetry {
		d, ok := s.devices[stateKey(t.SiteID, t.DeviceID)]
		if !ok || d.SiteID != siteID {
			continue
		}
		power := t.Metrics["power"]
		switch d.Type {
		case model.DevicePV:
			summary.PVPowerW += power
		case model.DeviceLoad:
			summary.LoadPowerW += power
		case model.DeviceBattery:
			summary.BatteryPower += power
			if soc, ok := t.Metrics["soc"]; ok {
				socSum += soc
				socCount++
			}
		}
		if ts := time.Unix(t.Timestamp, 0); ts.After(summary.LastUpdated) {
			summary.LastUpdated = ts
		}
	}
	if socCount > 0 {
		summary.AvgSOC = socSum / float64(socCount)
	}
	summary.NetPowerW = summary.LoadPowerW - summary.PVPowerW - summary.BatteryPower
	return summary
}

func (s *Store) DeviceStates(siteID string, now time.Time, onlineTTL time.Duration) []model.DeviceRuntimeState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.DeviceRuntimeState, 0, len(s.devices))
	for _, d := range s.devices {
		if d.SiteID != siteID {
			continue
		}
		state := model.DeviceRuntimeState{Device: d}
		if tele, ok := s.telemetry[stateKey(siteID, d.ID)]; ok {
			teleCopy := tele
			state.Telemetry = &teleCopy
			state.LastSeenAt = time.Unix(tele.Timestamp, 0)
			if !state.LastSeenAt.IsZero() {
				staleFor := now.Sub(state.LastSeenAt)
				if staleFor < 0 {
					staleFor = 0
				}
				state.StaleForSec = int64(staleFor.Seconds())
				state.Online = staleFor <= onlineTTL
			}
		}
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Device.Type == out[j].Device.Type {
			return out[i].Device.ID < out[j].Device.ID
		}
		return out[i].Device.Type < out[j].Device.Type
	})
	return out
}

func (s *Store) PutCommandIssued(siteID string, d model.Device, cmd model.Command) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands = append([]model.CommandRecord{{
		SiteID:     siteID,
		DeviceID:   d.ID,
		DeviceType: d.Type,
		Command:    cmd,
		Status:     "issued",
		UpdatedAt:  time.Now(),
	}}, s.commands...)
	if len(s.commands) > 200 {
		s.commands = s.commands[:200]
	}
}

func (s *Store) SetCommands(commands []model.CommandRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands = append([]model.CommandRecord(nil), commands...)
	if len(s.commands) > 200 {
		s.commands = s.commands[:200]
	}
}

func (s *Store) PutCommandAck(deviceID string, ack model.CommandAck) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.commands {
		if s.commands[i].DeviceID == deviceID && s.commands[i].Command.CommandID == ack.CommandID {
			s.commands[i].Ack = &ack
			if ack.OK {
				s.commands[i].Status = "acked"
			} else {
				s.commands[i].Status = "failed"
			}
			s.commands[i].UpdatedAt = time.Now()
			return
		}
	}
}

func (s *Store) Commands() []model.CommandRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.CommandRecord, len(s.commands))
	copy(out, s.commands)
	return out
}

func (s *Store) PutEvent(event model.DeviceEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append([]model.DeviceEvent{event}, s.events...)
	if len(s.events) > 200 {
		s.events = s.events[:200]
	}
}

func (s *Store) SetEvents(events []model.DeviceEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append([]model.DeviceEvent(nil), events...)
	if len(s.events) > 200 {
		s.events = s.events[:200]
	}
}

func (s *Store) Events() []model.DeviceEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.DeviceEvent, len(s.events))
	copy(out, s.events)
	return out
}

func (s *Store) loadTelemetry(ctx context.Context) error {
	values, err := s.redis.HGetAll(ctx, s.redisKey).Result()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for deviceID, raw := range values {
		var t model.Telemetry
		if err := json.Unmarshal([]byte(raw), &t); err != nil {
			log.Printf("skip bad redis telemetry device=%s err=%v", deviceID, err)
			continue
		}
		if t.DeviceID == "" {
			t.DeviceID = deviceID
		}
		if t.SiteID == "" {
			t.SiteID = s.siteID
		}
		s.telemetry[stateKey(t.SiteID, t.DeviceID)] = t
	}
	return nil
}

func (s *Store) persistTelemetry(t model.Telemetry) {
	if s.redis == nil {
		return
	}
	payload, err := json.Marshal(t)
	if err != nil {
		log.Printf("marshal telemetry failed device=%s err=%v", t.DeviceID, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.redis.HSet(ctx, s.redisKey, stateKey(t.SiteID, t.DeviceID), payload).Err(); err != nil {
		log.Printf("persist redis telemetry failed device=%s err=%v", t.DeviceID, err)
	}
}

func telemetryKey(siteID string) string {
	return fmt.Sprintf("vpp:%s:latest_telemetry", siteID)
}

func stateKey(siteID, id string) string {
	return siteID + "\x00" + id
}

func (s *Store) deviceByIDLocked(id string) (model.Device, bool) {
	var found model.Device
	matches := 0
	for _, d := range s.devices {
		if d.ID == id {
			found = d
			matches++
		}
	}
	if matches != 1 {
		return model.Device{}, false
	}
	return found, true
}
