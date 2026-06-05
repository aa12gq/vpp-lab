package topic

import "testing"

func TestParseTelemetryTopic(t *testing.T) {
	got, ok := Parse("vpp/home-lab/battery/battery_01/telemetry")
	if !ok {
		t.Fatal("expected parse ok")
	}
	if got.SiteID != "home-lab" || got.DeviceType != "battery" || got.DeviceID != "battery_01" || got.Kind != "telemetry" {
		t.Fatalf("unexpected parsed topic: %+v", got)
	}
}

func TestParseCommandAckTopic(t *testing.T) {
	got, ok := Parse("vpp/home-lab/load/load_01/command/ack")
	if !ok {
		t.Fatal("expected parse ok")
	}
	if got.Kind != "command/ack" {
		t.Fatalf("unexpected kind: %s", got.Kind)
	}
}
