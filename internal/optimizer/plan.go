package optimizer

import (
	"time"

	"vpp-lab/internal/model"
)

type Slot struct {
	StartAt       time.Time `json:"start_at"`
	EndAt         time.Time `json:"end_at"`
	BatteryMode   string    `json:"battery_mode"`
	TargetPowerW  float64   `json:"target_power_w"`
	ExpectedPrice float64   `json:"expected_price"`
}

func BuildSimpleDayPlan(now time.Time, summary model.SiteSummary) []Slot {
	start := now.Truncate(15 * time.Minute)
	out := make([]Slot, 0, 96)
	for i := 0; i < 96; i++ {
		s := start.Add(time.Duration(i) * 15 * time.Minute)
		hour := s.Hour()
		slot := Slot{StartAt: s, EndAt: s.Add(15 * time.Minute)}
		switch {
		case hour >= 10 && hour <= 15:
			slot.BatteryMode = "charge"
			slot.TargetPowerW = 50
			slot.ExpectedPrice = 0.35
		case hour >= 18 && hour <= 22:
			slot.BatteryMode = "discharge"
			slot.TargetPowerW = 50
			slot.ExpectedPrice = 0.85
		default:
			slot.BatteryMode = "idle"
			slot.ExpectedPrice = 0.55
		}
		if summary.AvgSOC < 0.3 {
			slot.BatteryMode = "charge"
		}
		out = append(out, slot)
	}
	return out
}
