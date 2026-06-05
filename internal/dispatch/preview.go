package dispatch

import (
	"fmt"
	"time"

	"vpp-lab/internal/model"
	"vpp-lab/internal/optimizer"
)

type Preview struct {
	GeneratedAt       time.Time         `json:"generated_at"`
	SiteID            string            `json:"site_id"`
	Summary           model.SiteSummary `json:"summary"`
	Slot              optimizer.Slot    `json:"slot"`
	TrackingErrorW    float64           `json:"tracking_error_w"`
	CandidateDeviceID string            `json:"candidate_device_id,omitempty"`
	CandidateCommand  *model.Command    `json:"candidate_command,omitempty"`
	SafeToApply       bool              `json:"safe_to_apply"`
	Reason            string            `json:"reason"`
	Plan              optimizer.Plan    `json:"plan"`
}

func BuildPreview(now time.Time, summary model.SiteSummary, devices []model.Device, cfg optimizer.Config) Preview {
	plan := optimizer.BuildDayAheadPlan(now, summary, cfg)
	slot, ok := optimizer.CurrentSlot(plan, now)
	preview := Preview{
		GeneratedAt: now,
		SiteID:      summary.SiteID,
		Summary:     summary,
		Slot:        slot,
		Plan:        plan,
		SafeToApply: false,
	}
	if !ok {
		preview.Reason = "no active plan slot"
		return preview
	}

	preview.TrackingErrorW = round2(summary.NetPowerW - slot.NetLoadW)
	if slot.BatteryMode == "idle" || slot.TargetPowerW == 0 {
		preview.Reason = "active slot does not require battery action"
		return preview
	}

	battery, ok := firstDevice(devices, summary.SiteID, model.DeviceBattery)
	if !ok {
		preview.Reason = "no battery device registered"
		return preview
	}

	preview.CandidateDeviceID = battery.ID
	preview.CandidateCommand = &model.Command{
		CommandID: fmt.Sprintf("%s-preview-%d", battery.ID, now.UnixNano()),
		Action:    "set_mode",
		Params: map[string]interface{}{
			"mode":           slot.BatteryMode,
			"target_power_w": slot.TargetPowerW,
		},
		IssuedAt: now.Unix(),
		Reason:   "day-ahead plan tracking preview: " + slot.Reason,
	}
	preview.Reason = "candidate command generated from active plan slot"
	return preview
}

func firstDevice(devices []model.Device, siteID string, typ model.DeviceType) (model.Device, bool) {
	for _, d := range devices {
		if d.SiteID == siteID && d.Type == typ {
			return d, true
		}
	}
	return model.Device{}, false
}

func round2(v float64) float64 {
	if v < 0 {
		return -round2(-v)
	}
	return float64(int(v*100+0.5)) / 100
}
