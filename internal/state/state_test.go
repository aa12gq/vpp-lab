package state

import (
	"testing"
	"time"

	"vpp-lab/internal/model"
)

func TestDeviceStatesReportsOnlineAndStaleDevices(t *testing.T) {
	now := time.Date(2026, 6, 5, 14, 0, 0, 0, time.UTC)
	store := NewStore()
	store.UpsertDevice(model.Device{ID: "battery_01", SiteID: "home-lab", Type: model.DeviceBattery})
	store.UpsertDevice(model.Device{ID: "load_01", SiteID: "home-lab", Type: model.DeviceLoad})
	store.UpsertDevice(model.Device{ID: "pv_remote", SiteID: "remote-lab", Type: model.DevicePV})
	store.PutTelemetry(model.Telemetry{
		DeviceID:  "battery_01",
		Timestamp: now.Add(-10 * time.Second).Unix(),
		Metrics:   map[string]float64{"power": -15, "soc": 0.6},
	})
	store.PutTelemetry(model.Telemetry{
		DeviceID:  "load_01",
		Timestamp: now.Add(-45 * time.Second).Unix(),
		Metrics:   map[string]float64{"power": 50},
	})

	states := store.DeviceStates("home-lab", now, 30*time.Second)
	if len(states) != 2 {
		t.Fatalf("expected 2 home-lab states, got %d", len(states))
	}
	byID := map[string]model.DeviceRuntimeState{}
	for _, state := range states {
		byID[state.Device.ID] = state
	}
	if !byID["battery_01"].Online {
		t.Fatalf("battery_01 should be online: %+v", byID["battery_01"])
	}
	if byID["battery_01"].StaleForSec != 10 {
		t.Fatalf("battery_01 stale seconds = %d", byID["battery_01"].StaleForSec)
	}
	if byID["load_01"].Online {
		t.Fatalf("load_01 should be offline: %+v", byID["load_01"])
	}
	if byID["load_01"].Telemetry == nil || byID["load_01"].Telemetry.Metrics["power"] != 50 {
		t.Fatalf("load_01 telemetry missing: %+v", byID["load_01"])
	}
	if _, ok := byID["pv_remote"]; ok {
		t.Fatalf("remote site device leaked into home-lab states")
	}
}
