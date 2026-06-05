package deviceauth

import (
	"strings"
	"testing"
	"time"
)

func TestSignAndVerifyPayload(t *testing.T) {
	now := time.Unix(1000, 0)
	topic := "vpp/home-lab/load/load_01/telemetry"
	payload := []byte(`{"device_id":"load_01","timestamp":1000,"seq":1}`)
	signed, err := SignPayload(topic, payload, "secret", now)
	if err != nil {
		t.Fatalf("sign payload: %v", err)
	}
	if !strings.Contains(string(signed), `"auth"`) {
		t.Fatalf("signed payload missing auth: %s", signed)
	}
	if err := VerifyPayload(topic, "load_01", signed, Keys{"load_01": "secret"}, now, time.Minute); err != nil {
		t.Fatalf("verify signed payload: %v", err)
	}
}

func TestVerifyPayloadRejectsTampering(t *testing.T) {
	now := time.Unix(1000, 0)
	topic := "vpp/home-lab/load/load_01/telemetry"
	signed, err := SignPayload(topic, []byte(`{"device_id":"load_01","power":10}`), "secret", now)
	if err != nil {
		t.Fatalf("sign payload: %v", err)
	}
	tampered := strings.Replace(string(signed), `"power":10`, `"power":20`, 1)
	if err := VerifyPayload(topic, "load_01", []byte(tampered), Keys{"load_01": "secret"}, now, time.Minute); err == nil {
		t.Fatalf("expected tampered payload to fail")
	}
}

func TestVerifyPayloadRejectsStaleTimestamp(t *testing.T) {
	signed, err := SignPayload("topic", []byte(`{"device_id":"load_01"}`), "secret", time.Unix(1000, 0))
	if err != nil {
		t.Fatalf("sign payload: %v", err)
	}
	err = VerifyPayload("topic", "load_01", signed, Keys{"load_01": "secret"}, time.Unix(2000, 0), time.Minute)
	if err == nil {
		t.Fatalf("expected stale timestamp to fail")
	}
}

func TestVerifyPayloadDisabledWithoutKeys(t *testing.T) {
	if err := VerifyPayload("topic", "load_01", []byte(`{}`), nil, time.Now(), time.Minute); err != nil {
		t.Fatalf("disabled auth should pass: %v", err)
	}
}

func TestParseKeys(t *testing.T) {
	keys, err := ParseKeys("load_01=secret,battery_01=battery-secret")
	if err != nil {
		t.Fatalf("parse keys: %v", err)
	}
	if keys["load_01"] != "secret" || keys["battery_01"] != "battery-secret" {
		t.Fatalf("unexpected keys: %+v", keys)
	}
	if _, err := ParseKeys("bad"); err == nil {
		t.Fatalf("expected malformed key error")
	}
}
