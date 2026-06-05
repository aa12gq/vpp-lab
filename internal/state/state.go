package state

import (
	"sync"
	"time"

	"vpp-lab/internal/model"
)

type Store struct {
	mu        sync.RWMutex
	devices   map[string]model.Device
	telemetry map[string]model.Telemetry
}

func NewStore() *Store {
	return &Store{
		devices:   make(map[string]model.Device),
		telemetry: make(map[string]model.Telemetry),
	}
}

func (s *Store) UpsertDevice(d model.Device) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now()
	}
	s.devices[d.ID] = d
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
	d, ok := s.devices[id]
	return d, ok
}

func (s *Store) PutTelemetry(t model.Telemetry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.telemetry[t.DeviceID] = t
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

func (s *Store) Summary(siteID string) model.SiteSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var summary model.SiteSummary
	summary.SiteID = siteID
	var socSum float64
	var socCount int
	for id, t := range s.telemetry {
		d, ok := s.devices[id]
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
