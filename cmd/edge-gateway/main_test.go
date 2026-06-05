package main

import "testing"

func TestParseKindSet(t *testing.T) {
	kinds := parseKindSet("telemetry, event,status,")
	for _, want := range []string{"telemetry", "event", "status"} {
		if !kinds[want] {
			t.Fatalf("missing kind %s in %+v", want, kinds)
		}
	}
	if kinds["command"] {
		t.Fatalf("command should not be captured by default-like set")
	}
}

func TestUpstreamTopic(t *testing.T) {
	topic := "vpp/home-lab/load/load_01/telemetry"
	if got := upstreamTopic(topic, ""); got != topic {
		t.Fatalf("unexpected no-prefix topic: %s", got)
	}
	if got := upstreamTopic(topic, "cloud"); got != "cloud/"+topic {
		t.Fatalf("unexpected prefixed topic: %s", got)
	}
	if got := upstreamTopic("/"+topic, "/cloud/"); got != "cloud/"+topic {
		t.Fatalf("unexpected normalized prefixed topic: %s", got)
	}
}
