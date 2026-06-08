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

func TestSchedulerShedsLoadWhenBatteryCannotDischarge(t *testing.T) {
	store := state.NewStore()
	store.UpsertDevice(model.Device{ID: "battery_01", SiteID: "home-lab", Type: model.DeviceBattery})
	store.UpsertDevice(model.Device{ID: "load_01", SiteID: "home-lab", Type: model.DeviceLoad, CriticalLoad: true})
	store.UpsertDevice(model.Device{ID: "load_02", SiteID: "home-lab", Type: model.DeviceLoad, CriticalLoad: false})
	now := time.Now().Unix()
	store.PutTelemetry(model.Telemetry{DeviceID: "battery_01", Timestamp: now, Metrics: map[string]float64{"power": 0, "soc": 0.2}})
	store.PutTelemetry(model.Telemetry{DeviceID: "load_01", Timestamp: now, Metrics: map[string]float64{"power": 40}})
	store.PutTelemetry(model.Telemetry{DeviceID: "load_02", Timestamp: now, Metrics: map[string]float64{"power": 60}})

	fake := &fakeCommander{}
	s := New("home-lab", store, fake, model.Policy{BatteryMinSOC: 0.2, BatteryMaxSOC: 0.9, LoadShedThreshold: 80})
	s.Tick(context.Background())

	if len(fake.commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(fake.commands))
	}
	if fake.devices[0].ID != "load_02" || fake.commands[0].Action != "set_relay" || fake.commands[0].Params["on"] != false {
		t.Fatalf("unexpected command device=%+v command=%+v", fake.devices[0], fake.commands[0])
	}
}

func TestSchedulerDischargesBatteryBeforeLoadShed(t *testing.T) {
	store := state.NewStore()
	store.UpsertDevice(model.Device{ID: "battery_01", SiteID: "home-lab", Type: model.DeviceBattery})
	store.UpsertDevice(model.Device{ID: "load_02", SiteID: "home-lab", Type: model.DeviceLoad, CriticalLoad: false})
	now := time.Now().Unix()
	store.PutTelemetry(model.Telemetry{DeviceID: "battery_01", Timestamp: now, Metrics: map[string]float64{"power": 0, "soc": 0.5}})
	store.PutTelemetry(model.Telemetry{DeviceID: "load_02", Timestamp: now, Metrics: map[string]float64{"power": 100}})

	fake := &fakeCommander{}
	s := New("home-lab", store, fake, model.Policy{BatteryMinSOC: 0.2, BatteryMaxSOC: 0.9, LoadShedThreshold: 80})
	s.Tick(context.Background())

	if len(fake.commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(fake.commands))
	}
	if fake.devices[0].ID != "battery_01" || fake.commands[0].Action != "set_mode" || fake.commands[0].Params["mode"] != "discharging" {
		t.Fatalf("unexpected command device=%+v command=%+v", fake.devices[0], fake.commands[0])
	}
}
