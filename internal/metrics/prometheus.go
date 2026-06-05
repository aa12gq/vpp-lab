package metrics

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"vpp-lab/internal/model"
)

const DeviceOnlineTTL = 30 * time.Second

type Snapshot struct {
	GeneratedAt time.Time
	SiteID      string
	Summary     model.SiteSummary
	Devices     []model.Device
	Telemetry   map[string]model.Telemetry
	Commands    []model.CommandRecord
	Events      []model.DeviceEvent
	AuditLogs   []model.AuditLog
	MQTTRejects map[string]uint64
}

func RenderPrometheus(s Snapshot) string {
	var b strings.Builder
	writeHelp(&b, "vpp_site_power_w", "Site power by component.")
	writeType(&b, "vpp_site_power_w", "gauge")
	writeMetric(&b, "vpp_site_power_w", map[string]string{"site_id": s.SiteID, "component": "pv"}, s.Summary.PVPowerW)
	writeMetric(&b, "vpp_site_power_w", map[string]string{"site_id": s.SiteID, "component": "load"}, s.Summary.LoadPowerW)
	writeMetric(&b, "vpp_site_power_w", map[string]string{"site_id": s.SiteID, "component": "battery"}, s.Summary.BatteryPower)
	writeMetric(&b, "vpp_site_power_w", map[string]string{"site_id": s.SiteID, "component": "net"}, s.Summary.NetPowerW)

	writeHelp(&b, "vpp_site_battery_soc", "Average site battery SOC ratio.")
	writeType(&b, "vpp_site_battery_soc", "gauge")
	writeMetric(&b, "vpp_site_battery_soc", map[string]string{"site_id": s.SiteID}, s.Summary.AvgSOC)

	writeHelp(&b, "vpp_device_online", "Device online state based on last telemetry timestamp.")
	writeType(&b, "vpp_device_online", "gauge")
	writeHelp(&b, "vpp_device_metric", "Latest device telemetry metric.")
	writeType(&b, "vpp_device_metric", "gauge")
	for _, d := range sortedDevices(s.Devices) {
		tele, ok := s.Telemetry[d.ID]
		online := 0.0
		if ok && s.GeneratedAt.Sub(time.Unix(tele.Timestamp, 0)) <= DeviceOnlineTTL {
			online = 1
		}
		baseLabels := map[string]string{"site_id": d.SiteID, "device_id": d.ID, "device_type": string(d.Type)}
		writeMetric(&b, "vpp_device_online", baseLabels, online)
		for _, key := range sortedMetricKeys(tele.Metrics) {
			labels := map[string]string{"site_id": d.SiteID, "device_id": d.ID, "device_type": string(d.Type), "metric": key}
			writeMetric(&b, "vpp_device_metric", labels, tele.Metrics[key])
		}
	}

	writeHelp(&b, "vpp_command_records_total", "Recent command records by status.")
	writeType(&b, "vpp_command_records_total", "gauge")
	for status, count := range commandStatusCounts(s.Commands) {
		writeMetric(&b, "vpp_command_records_total", map[string]string{"site_id": s.SiteID, "status": status}, float64(count))
	}
	writeHelp(&b, "vpp_device_events_total", "Recent device events by severity.")
	writeType(&b, "vpp_device_events_total", "gauge")
	for severity, count := range eventSeverityCounts(s.Events) {
		writeMetric(&b, "vpp_device_events_total", map[string]string{"site_id": s.SiteID, "severity": severity}, float64(count))
	}
	writeHelp(&b, "vpp_audit_logs_total", "Recent control operation audit logs by action and status code.")
	writeType(&b, "vpp_audit_logs_total", "gauge")
	for key, count := range auditLogCounts(s.AuditLogs) {
		writeMetric(&b, "vpp_audit_logs_total", map[string]string{"site_id": s.SiteID, "action": key.action, "status_code": key.statusCode}, float64(count))
	}
	writeHelp(&b, "vpp_mqtt_rejected_messages_total", "MQTT messages rejected by the platform.")
	writeType(&b, "vpp_mqtt_rejected_messages_total", "counter")
	for _, reason := range sortedRejectReasons(s.MQTTRejects) {
		writeMetric(&b, "vpp_mqtt_rejected_messages_total", map[string]string{"site_id": s.SiteID, "reason": reason}, float64(s.MQTTRejects[reason]))
	}
	return b.String()
}

func writeHelp(b *strings.Builder, name, help string) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
}

func writeType(b *strings.Builder, name, typ string) {
	fmt.Fprintf(b, "# TYPE %s %s\n", name, typ)
}

func writeMetric(b *strings.Builder, name string, labels map[string]string, value float64) {
	fmt.Fprintf(b, "%s{%s} %.6f\n", name, renderLabels(labels), value)
}

func renderLabels(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, k, escapeLabel(labels[k])))
	}
	return strings.Join(parts, ",")
}

func escapeLabel(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return v
}

func sortedDevices(devices []model.Device) []model.Device {
	out := append([]model.Device(nil), devices...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].SiteID == out[j].SiteID {
			return out[i].ID < out[j].ID
		}
		return out[i].SiteID < out[j].SiteID
	})
	return out
}

func sortedMetricKeys(metrics map[string]float64) []string {
	keys := make([]string, 0, len(metrics))
	for k := range metrics {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func commandStatusCounts(commands []model.CommandRecord) map[string]int {
	counts := map[string]int{"issued": 0, "acked": 0, "failed": 0}
	for _, cmd := range commands {
		if _, ok := counts[cmd.Status]; !ok {
			counts[cmd.Status] = 0
		}
		counts[cmd.Status]++
	}
	return counts
}

func eventSeverityCounts(events []model.DeviceEvent) map[string]int {
	counts := map[string]int{"info": 0, "warning": 0, "critical": 0}
	for _, event := range events {
		severity := event.Severity
		if severity == "" {
			severity = "info"
		}
		if _, ok := counts[severity]; !ok {
			counts[severity] = 0
		}
		counts[severity]++
	}
	return counts
}

type auditLogKey struct {
	action     string
	statusCode string
}

func auditLogCounts(logs []model.AuditLog) map[auditLogKey]int {
	counts := make(map[auditLogKey]int)
	for _, log := range logs {
		action := log.Action
		if action == "" {
			action = "unknown"
		}
		statusCode := fmt.Sprintf("%d", log.StatusCode)
		if log.StatusCode == 0 {
			statusCode = "0"
		}
		counts[auditLogKey{action: action, statusCode: statusCode}]++
	}
	return counts
}

func sortedRejectReasons(rejects map[string]uint64) []string {
	reasons := make([]string, 0, len(rejects))
	for reason := range rejects {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	return reasons
}
