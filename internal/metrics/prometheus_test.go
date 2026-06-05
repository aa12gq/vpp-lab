package metrics

import (
	"strings"
	"testing"
	"time"

	"vpp-lab/internal/model"
)

func TestRenderPrometheusIncludesDeviceAndCommandMetrics(t *testing.T) {
	now := time.Date(2026, 6, 5, 13, 0, 0, 0, time.UTC)
	out := RenderPrometheus(Snapshot{
		GeneratedAt: now,
		SiteID:      "home-lab",
		Summary: model.SiteSummary{
			SiteID:     "home-lab",
			PVPowerW:   90,
			LoadPowerW: 70,
			AvgSOC:     0.6,
		},
		Devices: []model.Device{{ID: "battery_01", SiteID: "home-lab", Type: model.DeviceBattery}},
		Telemetry: map[string]model.Telemetry{
			"battery_01": {
				Timestamp: now.Unix(),
				Metrics:   map[string]float64{"power": -15, "soc": 0.6},
			},
		},
		Commands: []model.CommandRecord{{Status: "acked"}},
		AuditLogs: []model.AuditLog{
			{Action: "policy.update", StatusCode: 200},
			{Action: "policy.update", StatusCode: 401},
		},
	})
	for _, want := range []string{
		`vpp_site_power_w{component="pv",site_id="home-lab"} 90.000000`,
		`vpp_device_online{device_id="battery_01",device_type="battery",site_id="home-lab"} 1.000000`,
		`vpp_device_metric{device_id="battery_01",device_type="battery",metric="soc",site_id="home-lab"} 0.600000`,
		`vpp_command_records_total{site_id="home-lab",status="acked"} 1.000000`,
		`vpp_audit_logs_total{action="policy.update",site_id="home-lab",status_code="200"} 1.000000`,
		`vpp_audit_logs_total{action="policy.update",site_id="home-lab",status_code="401"} 1.000000`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing metric %q in:\n%s", want, out)
		}
	}
}

func TestRenderPrometheusMarksStaleDeviceOffline(t *testing.T) {
	now := time.Date(2026, 6, 5, 13, 0, 0, 0, time.UTC)
	out := RenderPrometheus(Snapshot{
		GeneratedAt: now,
		SiteID:      "home-lab",
		Devices:     []model.Device{{ID: "load_01", SiteID: "home-lab", Type: model.DeviceLoad}},
		Telemetry: map[string]model.Telemetry{
			"load_01": {Timestamp: now.Add(-DeviceOnlineTTL - time.Second).Unix()},
		},
	})
	want := `vpp_device_online{device_id="load_01",device_type="load",site_id="home-lab"} 0.000000`
	if !strings.Contains(out, want) {
		t.Fatalf("missing offline metric %q in:\n%s", want, out)
	}
}
