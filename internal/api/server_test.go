package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"vpp-lab/internal/model"
	"vpp-lab/internal/scheduler"
	"vpp-lab/internal/state"
)

type fakePublisher struct{}

func (fakePublisher) PublishCommand(string, model.Device, model.Command) error { return nil }

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
