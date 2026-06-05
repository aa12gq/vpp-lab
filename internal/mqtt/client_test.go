package mqtt

import (
	"testing"

	"vpp-lab/internal/config"
)

func TestRequiresDeviceAuth(t *testing.T) {
	for _, kind := range []string{"telemetry", "status", "event", "command/ack"} {
		if !requiresDeviceAuth(kind) {
			t.Fatalf("expected %s to require auth", kind)
		}
	}
	if requiresDeviceAuth("command") {
		t.Fatalf("platform command should not require device auth")
	}
}

func TestRejectedMessagesReturnsCopy(t *testing.T) {
	client := NewClient(config.Config{}, nil, nil)
	client.incrementRejected("auth")

	first := client.RejectedMessages()
	if first["auth"] != 1 {
		t.Fatalf("unexpected reject count: %+v", first)
	}
	first["auth"] = 100

	second := client.RejectedMessages()
	if second["auth"] != 1 {
		t.Fatalf("rejected messages should return a copy, got %+v", second)
	}
}
