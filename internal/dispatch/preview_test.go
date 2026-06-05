package dispatch

import (
	"testing"
	"time"

	"vpp-lab/internal/model"
	"vpp-lab/internal/optimizer"
)

func TestBuildPreviewCreatesCandidateForBatterySlot(t *testing.T) {
	now := time.Date(2026, 6, 5, 18, 0, 0, 0, time.UTC)
	preview := BuildPreview(now, model.SiteSummary{
		SiteID:     "home-lab",
		PVPowerW:   10,
		LoadPowerW: 100,
		AvgSOC:     0.8,
		NetPowerW:  90,
	}, []model.Device{
		{ID: "battery_01", SiteID: "home-lab", Type: model.DeviceBattery},
	}, optimizer.Config{HorizonHours: 1, SlotMinutes: 15, BatteryCapacityWh: 150, BatteryPowerLimitW: 50, MinSOC: 0.25, MaxSOC: 0.9})
	if preview.CandidateCommand == nil {
		t.Fatal("expected candidate command")
	}
	if preview.CandidateCommand.Action != "set_mode" {
		t.Fatalf("unexpected action: %s", preview.CandidateCommand.Action)
	}
	if preview.SafeToApply {
		t.Fatal("preview must not be safe to apply by default")
	}
}

func TestBuildPreviewSkipsWithoutBattery(t *testing.T) {
	now := time.Date(2026, 6, 5, 18, 0, 0, 0, time.UTC)
	preview := BuildPreview(now, model.SiteSummary{
		SiteID:     "home-lab",
		PVPowerW:   10,
		LoadPowerW: 100,
		AvgSOC:     0.8,
		NetPowerW:  90,
	}, nil, optimizer.Config{HorizonHours: 1, SlotMinutes: 15, BatteryCapacityWh: 150, BatteryPowerLimitW: 50, MinSOC: 0.25, MaxSOC: 0.9})
	if preview.CandidateCommand != nil {
		t.Fatal("expected no candidate command")
	}
}

func TestDecideApplyRequiresConfirm(t *testing.T) {
	decision := DecideApply(Preview{
		CandidateDeviceID: "battery_01",
		CandidateCommand:  &model.Command{Action: "set_mode"},
	}, ApplyRequest{})
	if decision.CanApply {
		t.Fatal("expected apply rejected without confirm")
	}
}

func TestDecideApplyRejectsLargeTrackingError(t *testing.T) {
	decision := DecideApply(Preview{
		CandidateDeviceID: "battery_01",
		CandidateCommand:  &model.Command{Action: "set_mode"},
		TrackingErrorW:    200,
	}, ApplyRequest{Confirm: true, MaxAbsTrackingErrorW: 50})
	if decision.CanApply {
		t.Fatal("expected apply rejected by tracking error limit")
	}
}
