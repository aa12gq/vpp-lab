package optimizer

import (
	"testing"
	"time"

	"vpp-lab/internal/model"
)

func TestBuildDayAheadPlanUsesConfiguredHorizon(t *testing.T) {
	plan := BuildDayAheadPlan(time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC), model.SiteSummary{
		SiteID:     "home-lab",
		PVPowerW:   90,
		LoadPowerW: 70,
		AvgSOC:     0.5,
	}, Config{HorizonHours: 1, SlotMinutes: 15, BatteryCapacityWh: 150, BatteryPowerLimitW: 50, MinSOC: 0.25, MaxSOC: 0.9})
	if len(plan.Slots) != 4 {
		t.Fatalf("expected 4 slots, got %d", len(plan.Slots))
	}
	if plan.Slots[0].ForecastSOC < plan.Config.MinSOC || plan.Slots[0].ForecastSOC > plan.Config.MaxSOC {
		t.Fatalf("soc out of range: %.4f", plan.Slots[0].ForecastSOC)
	}
}

func TestPeakPriceDischargesWhenLoadDeficit(t *testing.T) {
	plan := BuildDayAheadPlan(time.Date(2026, 6, 5, 18, 0, 0, 0, time.UTC), model.SiteSummary{
		SiteID:     "home-lab",
		PVPowerW:   10,
		LoadPowerW: 100,
		AvgSOC:     0.8,
	}, Config{HorizonHours: 1, SlotMinutes: 15, BatteryCapacityWh: 150, BatteryPowerLimitW: 50, MinSOC: 0.25, MaxSOC: 0.9})
	if plan.Slots[0].BatteryMode != "discharge" {
		t.Fatalf("expected discharge at peak deficit, got %s", plan.Slots[0].BatteryMode)
	}
}

func TestMissingSOCDefaultsToHalf(t *testing.T) {
	plan := BuildDayAheadPlan(time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC), model.SiteSummary{
		SiteID:     "home-lab",
		PVPowerW:   0,
		LoadPowerW: 0,
		AvgSOC:     0,
	}, Config{HorizonHours: 1, SlotMinutes: 15, BatteryCapacityWh: 150, BatteryPowerLimitW: 50, MinSOC: 0.25, MaxSOC: 0.9})
	if plan.Slots[0].ForecastSOC < 0.49 {
		t.Fatalf("expected default soc around 0.5, got %.4f", plan.Slots[0].ForecastSOC)
	}
}

func TestCurrentSlot(t *testing.T) {
	now := time.Date(2026, 6, 5, 12, 7, 0, 0, time.UTC)
	plan := BuildDayAheadPlan(now, model.SiteSummary{SiteID: "home-lab", AvgSOC: 0.5}, Config{HorizonHours: 1, SlotMinutes: 15, BatteryCapacityWh: 150, BatteryPowerLimitW: 50, MinSOC: 0.25, MaxSOC: 0.9})
	slot, ok := CurrentSlot(plan, now)
	if !ok {
		t.Fatal("expected current slot")
	}
	if now.Before(slot.StartAt) || !now.Before(slot.EndAt) {
		t.Fatalf("slot does not contain now: %+v", slot)
	}
}

func TestSingleTariffDoesNotTriggerArbitrage(t *testing.T) {
	plan := BuildDayAheadPlan(time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC), model.SiteSummary{
		SiteID:     "home-lab",
		PVPowerW:   0,
		LoadPowerW: 20,
		AvgSOC:     0.5,
	}, Config{
		HorizonHours:       1,
		SlotMinutes:        15,
		BatteryCapacityWh:  150,
		BatteryPowerLimitW: 50,
		MinSOC:             0.25,
		MaxSOC:             0.9,
		Tariffs:            []TariffBand{{Name: "flat", StartHour: 0, EndHour: 24, Price: 0.58}},
	})
	if plan.Slots[0].Reason == "low tariff" || plan.Slots[0].Reason == "peak tariff and load deficit" {
		t.Fatalf("single tariff should not trigger arbitrage: %+v", plan.Slots[0])
	}
}
