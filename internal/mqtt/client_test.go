package mqtt

import "testing"

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
