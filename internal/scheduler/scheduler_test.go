package scheduler

import (
	"context"
	"sync"
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

func TestValidatePolicyRejectsInvalidValues(t *testing.T) {
	tests := []model.Policy{
		{BatteryMinSOC: -0.1, BatteryMaxSOC: 0.9, LoadShedThreshold: 80},
		{BatteryMinSOC: 0.2, BatteryMaxSOC: 1.1, LoadShedThreshold: 80},
		{BatteryMinSOC: 0.9, BatteryMaxSOC: 0.2, LoadShedThreshold: 80},
		{BatteryMinSOC: 0.2, BatteryMaxSOC: 0.9, LoadShedThreshold: -1},
	}
	for _, tt := range tests {
		if err := ValidatePolicy(tt); err == nil {
			t.Fatalf("expected invalid policy: %+v", tt)
		}
	}
}

func TestSchedulerPolicyConcurrentAccess(t *testing.T) {
	store := state.NewStore()
	s := New("home-lab", store, &fakeCommander{}, model.Policy{BatteryMinSOC: 0.2, BatteryMaxSOC: 0.9, LoadShedThreshold: 80})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			s.SetPolicy(model.Policy{BatteryMinSOC: 0.1, BatteryMaxSOC: 0.9, LoadShedThreshold: float64(80 + i)})
		}(i)
		go func() {
			defer wg.Done()
			_ = s.Policy()
		}()
	}
	wg.Wait()
}

func TestSchedulerRunIgnoresInvalidInterval(t *testing.T) {
	store := state.NewStore()
	s := New("home-lab", store, &fakeCommander{}, model.Policy{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(context.Background(), 0)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler should return immediately for invalid interval")
	}
}
