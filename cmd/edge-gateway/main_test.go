package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseKindSet(t *testing.T) {
	kinds := parseKindSet("telemetry, event,status,command/ack,")
	for _, want := range []string{"telemetry", "event", "status", "command/ack"} {
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

func TestEdgeSubscribeTopics(t *testing.T) {
	got := edgeSubscribeTopics("home-lab")
	want := []string{"vpp/home-lab/+/+/+", "vpp/home-lab/+/+/command/ack"}
	if len(got) != len(want) {
		t.Fatalf("unexpected topic count: %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("topic %d mismatch: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestEdgeAuthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/local-command", nil)
	if edgeAuthorized(req, "") {
		t.Fatalf("empty token should reject local command")
	}
	if edgeAuthorized(req, "secret") {
		t.Fatalf("missing token should reject")
	}
	req.Header.Set("X-VPP-Edge-Token", "secret")
	if !edgeAuthorized(req, "secret") {
		t.Fatalf("edge token header should allow")
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/local-command", nil)
	req.Header.Set("Authorization", "Bearer secret")
	if !edgeAuthorized(req, "secret") {
		t.Fatalf("bearer token should allow")
	}
}

func TestDecodeLocalCommand(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/local-command", strings.NewReader(`{
		"device_type":"load",
		"device_id":"load_02",
		"action":"set_relay",
		"params":{"on":true}
	}`))
	cmd, err := decodeLocalCommand(req)
	if err != nil {
		t.Fatalf("decode local command: %v", err)
	}
	if cmd.DeviceType != "load" || cmd.DeviceID != "load_02" || cmd.Action != "set_relay" {
		t.Fatalf("unexpected command: %+v", cmd)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/local-command", strings.NewReader(`{"device_id":"load_02"}`))
	if _, err := decodeLocalCommand(req); err == nil {
		t.Fatalf("expected missing fields error")
	}
}

func TestEdgeCounterSnapshot(t *testing.T) {
	counter := newEdgeCounter("accepted", "failed")
	counter.Inc("failed")
	counter.Inc("accepted")
	counter.Inc("accepted")

	got := counter.Snapshot()
	if len(got) != 2 {
		t.Fatalf("expected 2 counter values, got %+v", got)
	}
	if got[0].Label != "accepted" || got[0].Value != 2 {
		t.Fatalf("unexpected accepted counter: %+v", got)
	}
	if got[1].Label != "failed" || got[1].Value != 1 {
		t.Fatalf("unexpected failed counter: %+v", got)
	}
}
