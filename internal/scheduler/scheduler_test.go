package scheduler

import (
	"context"
	"testing"
	"time"

	"vpp-lab/internal/model"
	"vpp-lab/internal/state"
)

type fakeCommander struct {
	commands []model.Command
	devices  []model.Device
}

func (f *fakeCommander) PublishCommand(_ string, d model.Device, cmd model.Command) error {
	f.devices = append(f.devices, d)
	f.commands = append(f.commands, cmd)
	return nil
}

func TestSchedulerChargesBatteryOnPVSurplus(t *testing.T) {
	store := state.NewStore()
	store.UpsertDevice(model.Device{ID: "pv_01", SiteID: "home-lab", Type: model.DevicePV})
	store.UpsertDevice(model.Device{ID: "battery_01", SiteID: "home-lab", Type: model.DeviceBattery})
	now := time.Now().Unix()
	store.PutTelemetry(model.Telemetry{DeviceID: "pv_01", Timestamp: now, Metrics: map[string]float64{"power": 100}})
	store.PutTelemetry(model.Telemetry{DeviceID: "battery_01", Timestamp: now, Metrics: map[string]float64{"power": 0, "soc": 0.5}})

	fake := &fakeCommander{}
	s := New("home-lab", store, fake, model.Policy{BatteryMinSOC: 0.2, BatteryMaxSOC: 0.9, LoadShedThreshold: 80})
	s.Tick(context.Background())

	if len(fake.commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(fake.commands))
	}
	if fake.commands[0].Action != "set_mode" || fake.commands[0].Params["mode"] != "charging" {
		t.Fatalf("unexpected command: %+v", fake.commands[0])
	}
}
