package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"vpp-lab/internal/model"
	"vpp-lab/internal/scheduler"
	"vpp-lab/internal/state"
)

type fakePublisher struct{}

func (fakePublisher) PublishCommand(string, model.Device, model.Command) error { return nil }

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
