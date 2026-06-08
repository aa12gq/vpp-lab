package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"vpp-lab/internal/model"
	"vpp-lab/internal/scheduler"
	"vpp-lab/internal/state"
)

type fakePublisher struct{}

func (fakePublisher) PublishCommand(string, model.Device, model.Command) error { return nil }

type recordingPublisher struct {
	siteID string
	device model.Device
	cmd    model.Command
}

func (p *recordingPublisher) PublishCommand(siteID string, d model.Device, cmd model.Command) error {
	p.siteID = siteID
	p.device = d
	p.cmd = cmd
	return nil
}

type fakeAuditRecorder struct {
	logs []model.AuditLog
}

func (f *fakeAuditRecorder) PutAuditLog(_ context.Context, log model.AuditLog) error {
	f.logs = append(f.logs, log)
	return nil
}

func (f *fakeAuditRecorder) ListAuditLogs(context.Context, int) ([]model.AuditLog, error) {
	out := make([]model.AuditLog, len(f.logs))
	copy(out, f.logs)
	return out, nil
}

type failingDeviceSaver struct{}

func (failingDeviceSaver) Upsert(context.Context, model.Device) error {
	return errors.New("persist failed")
}

func TestControlTokenProtectsCommandEndpoint(t *testing.T) {
	store := state.NewStore()
	store.UpsertDevice(model.Device{ID: "load_01", SiteID: "home-lab", Type: model.DeviceLoad})
	sch := scheduler.New("home-lab", store, fakePublisher{}, model.Policy{})
	handler := New("home-lab", store, sch, fakePublisher{}, nil).WithControlToken("secret").Handler()

	body := []byte(`{"action":"set_relay","params":{"on":false}}`)
	unauthorized := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/load_01/command", bytes.NewReader(body))
	handler.ServeHTTP(unauthorized, req)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without token, got %d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	authorized := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/devices/load_01/command", bytes.NewReader(body))
	req.Header.Set("X-VPP-Control-Token", "secret")
	handler.ServeHTTP(authorized, req)
	if authorized.Code != http.StatusAccepted {
		t.Fatalf("expected accepted with token, got %d body=%s", authorized.Code, authorized.Body.String())
	}
}

func TestControlTokenAcceptsBearerHeader(t *testing.T) {
	store := state.NewStore()
	store.UpsertDevice(model.Device{ID: "load_01", SiteID: "home-lab", Type: model.DeviceLoad})
	sch := scheduler.New("home-lab", store, fakePublisher{}, model.Policy{})
	handler := New("home-lab", store, sch, fakePublisher{}, nil).WithControlToken("secret").Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/load_01/command", bytes.NewReader([]byte(`{"action":"set_relay","params":{"on":true}}`)))
	req.Header.Set("Authorization", "Bearer secret")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected accepted with bearer token, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSiteCommandRouteUsesSiteScopedDevice(t *testing.T) {
	store := state.NewStore()
	store.UpsertDevice(model.Device{ID: "load_01", SiteID: "home-lab", Type: model.DeviceLoad})
	store.UpsertDevice(model.Device{ID: "load_01", SiteID: "remote-lab", Type: model.DeviceLoad})
	pub := &recordingPublisher{}
	sch := scheduler.New("home-lab", store, pub, model.Policy{})
	handler := New("home-lab", store, sch, pub, nil).Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sites/remote-lab/devices/load_01/command", bytes.NewReader([]byte(`{"action":"set_relay","params":{"on":false}}`)))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected accepted, got %d body=%s", rec.Code, rec.Body.String())
	}
	if pub.siteID != "remote-lab" || pub.device.SiteID != "remote-lab" {
		t.Fatalf("command should use remote-lab device, got site=%q device=%+v cmd=%+v", pub.siteID, pub.device, pub.cmd)
	}
}

func TestLegacyCommandRouteUsesDefaultSiteWhenDeviceIDIsDuplicated(t *testing.T) {
	store := state.NewStore()
	store.UpsertDevice(model.Device{ID: "load_01", SiteID: "home-lab", Type: model.DeviceLoad})
	store.UpsertDevice(model.Device{ID: "load_01", SiteID: "remote-lab", Type: model.DeviceLoad})
	pub := &recordingPublisher{}
	sch := scheduler.New("home-lab", store, pub, model.Policy{})
	handler := New("home-lab", store, sch, pub, nil).Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/load_01/command", bytes.NewReader([]byte(`{"action":"set_relay","params":{"on":false}}`)))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected accepted, got %d body=%s", rec.Code, rec.Body.String())
	}
	if pub.siteID != "home-lab" || pub.device.SiteID != "home-lab" {
		t.Fatalf("legacy command should use default site, got site=%q device=%+v", pub.siteID, pub.device)
	}
}

func TestAuditRecordsControlRequests(t *testing.T) {
	store := state.NewStore()
	store.UpsertDevice(model.Device{ID: "load_01", SiteID: "home-lab", Type: model.DeviceLoad})
	sch := scheduler.New("home-lab", store, fakePublisher{}, model.Policy{})
	audit := &fakeAuditRecorder{}
	handler := New("home-lab", store, sch, fakePublisher{}, nil).
		WithControlToken("secret").
		WithAuditRecorder(audit).
		Handler()

	body := []byte(`{"action":"set_relay","params":{"on":false}}`)
	unauthorized := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/load_01/command", bytes.NewReader(body))
	req.Header.Set("X-VPP-Actor", "lab-user")
	handler.ServeHTTP(unauthorized, req)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", unauthorized.Code)
	}

	authorized := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/devices/load_01/command", bytes.NewReader(body))
	req.Header.Set("X-VPP-Control-Token", "secret")
	req.Header.Set("X-VPP-Actor", "lab-user")
	handler.ServeHTTP(authorized, req)
	if authorized.Code != http.StatusAccepted {
		t.Fatalf("expected accepted, got %d body=%s", authorized.Code, authorized.Body.String())
	}

	if len(audit.logs) != 2 {
		t.Fatalf("expected 2 audit logs, got %+v", audit.logs)
	}
	if audit.logs[0].Action != "command.issue" || audit.logs[0].StatusCode != http.StatusUnauthorized {
		t.Fatalf("unexpected unauthorized audit log: %+v", audit.logs[0])
	}
	if audit.logs[1].Action != "command.issue" || audit.logs[1].StatusCode != http.StatusAccepted {
		t.Fatalf("unexpected authorized audit log: %+v", audit.logs[1])
	}
	if audit.logs[1].Actor != "lab-user" {
		t.Fatalf("unexpected actor: %+v", audit.logs[1])
	}
}

func TestUpsertDeviceDoesNotMutateStoreWhenPersistenceFails(t *testing.T) {
	store := state.NewStore()
	sch := scheduler.New("home-lab", store, fakePublisher{}, model.Policy{})
	handler := New("home-lab", store, sch, fakePublisher{}, failingDeviceSaver{}).Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices", bytes.NewReader([]byte(`{"id":"load_99","type":"load","name":"Bad Save"}`)))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := store.Device("load_99"); ok {
		t.Fatalf("device should not be in memory after persistence failure")
	}
}

func TestSetPolicyRejectsInvalidPolicy(t *testing.T) {
	store := state.NewStore()
	sch := scheduler.New("home-lab", store, fakePublisher{}, model.Policy{BatteryMinSOC: 0.2, BatteryMaxSOC: 0.9, LoadShedThreshold: 80})
	handler := New("home-lab", store, sch, fakePublisher{}, nil).WithControlToken("secret").Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/policies/default", bytes.NewReader([]byte(`{"battery_min_soc":0.95,"battery_max_soc":0.2,"load_shed_threshold_w":80}`)))
	req.Header.Set("X-VPP-Control-Token", "secret")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := sch.Policy(); got.BatteryMinSOC != 0.2 || got.BatteryMaxSOC != 0.9 || got.LoadShedThreshold != 80 {
		t.Fatalf("policy should remain unchanged: %+v", got)
	}
}

func TestCustomPlanRejectsInvalidConfig(t *testing.T) {
	store := state.NewStore()
	sch := scheduler.New("home-lab", store, fakePublisher{}, model.Policy{})
	handler := New("home-lab", store, sch, fakePublisher{}, nil).Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sites/home-lab/plan", bytes.NewReader([]byte(`{"slot_minutes":-15}`)))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApplyDispatchRejectsInvalidConfig(t *testing.T) {
	store := state.NewStore()
	store.UpsertDevice(model.Device{ID: "battery_01", SiteID: "home-lab", Type: model.DeviceBattery})
	sch := scheduler.New("home-lab", store, fakePublisher{}, model.Policy{})
	handler := New("home-lab", store, sch, fakePublisher{}, nil).Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sites/home-lab/dispatch/apply", bytes.NewReader([]byte(`{"confirm":true,"max_abs_tracking_error_w":-1}`)))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}
