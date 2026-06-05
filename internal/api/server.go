package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"vpp-lab/internal/dispatch"
	"vpp-lab/internal/metrics"
	"vpp-lab/internal/model"
	"vpp-lab/internal/optimizer"
	"vpp-lab/internal/scheduler"
	"vpp-lab/internal/state"
)

type CommandPublisher interface {
	PublishCommand(siteID string, d model.Device, cmd model.Command) error
}

type DeviceSaver interface {
	Upsert(rctx context.Context, d model.Device) error
}

type HealthCheck struct {
	Name  string
	Check func(context.Context) error
}

//go:embed ui/index.html
var consoleHTML string

type Server struct {
	siteID    string
	store     *state.Store
	scheduler *scheduler.Scheduler
	publisher CommandPublisher
	devices   DeviceSaver
	checks    []HealthCheck
	token     string
}

func New(siteID string, store *state.Store, sch *scheduler.Scheduler, publisher CommandPublisher, devices DeviceSaver, checks ...HealthCheck) *Server {
	return &Server{siteID: siteID, store: store, scheduler: sch, publisher: publisher, devices: devices, checks: checks}
}

func (s *Server) WithControlToken(token string) *Server {
	s.token = strings.TrimSpace(token)
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.console)
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /metrics", s.metrics)
	mux.HandleFunc("GET /api/v1/devices", s.listDevices)
	mux.HandleFunc("POST /api/v1/devices", s.upsertDevice)
	mux.HandleFunc("GET /api/v1/sites/{site_id}/summary", s.summary)
	mux.HandleFunc("GET /api/v1/sites/{site_id}/device-states", s.deviceStates)
	mux.HandleFunc("GET /api/v1/sites/{site_id}/plan", s.plan)
	mux.HandleFunc("POST /api/v1/sites/{site_id}/plan", s.customPlan)
	mux.HandleFunc("GET /api/v1/sites/{site_id}/dispatch-preview", s.dispatchPreview)
	mux.HandleFunc("POST /api/v1/sites/{site_id}/dispatch-preview", s.customDispatchPreview)
	mux.HandleFunc("POST /api/v1/sites/{site_id}/dispatch/apply", s.applyDispatch)
	mux.HandleFunc("GET /api/v1/commands", s.commands)
	mux.HandleFunc("GET /api/v1/events", s.events)
	mux.HandleFunc("GET /api/v1/policies/default", s.getPolicy)
	mux.HandleFunc("PUT /api/v1/policies/default", s.setPolicy)
	mux.HandleFunc("POST /api/v1/devices/{device_id}/command", s.command)
	return withJSON(mux)
}

func (s *Server) console(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(consoleHTML))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	statusCode := http.StatusOK
	status := "ok"
	checks := make(map[string]string, len(s.checks))
	for _, check := range s.checks {
		if check.Name == "" || check.Check == nil {
			continue
		}
		if err := check.Check(ctx); err != nil {
			statusCode = http.StatusServiceUnavailable
			status = "degraded"
			checks[check.Name] = err.Error()
			continue
		}
		checks[check.Name] = "ok"
	}
	writeJSON(w, statusCode, map[string]interface{}{"status": status, "checks": checks})
}

func (s *Server) metrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(metrics.RenderPrometheus(metrics.Snapshot{
		GeneratedAt: time.Now(),
		SiteID:      s.siteID,
		Summary:     s.store.Summary(s.siteID),
		Devices:     s.store.Devices(),
		Telemetry:   s.store.Telemetry(),
		Commands:    s.store.Commands(),
		Events:      s.store.Events(),
	})))
}

func (s *Server) listDevices(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.Devices())
}

func (s *Server) upsertDevice(w http.ResponseWriter, r *http.Request) {
	if !s.requireControl(w, r) {
		return
	}
	var d model.Device
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.SiteID == "" {
		d.SiteID = s.siteID
	}
	if d.ID == "" || d.Type == "" {
		writeError(w, http.StatusBadRequest, "id and type are required")
		return
	}
	if d.Name == "" {
		d.Name = d.ID
	}
	s.store.UpsertDevice(d)
	if s.devices != nil {
		if err := s.devices.Upsert(r.Context(), d); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	siteID := r.PathValue("site_id")
	if siteID == "" {
		siteID = s.siteID
	}
	writeJSON(w, http.StatusOK, s.store.Summary(siteID))
}

func (s *Server) deviceStates(w http.ResponseWriter, r *http.Request) {
	siteID := r.PathValue("site_id")
	if siteID == "" {
		siteID = s.siteID
	}
	writeJSON(w, http.StatusOK, s.store.DeviceStates(siteID, time.Now(), metrics.DeviceOnlineTTL))
}

func (s *Server) plan(w http.ResponseWriter, r *http.Request) {
	siteID := r.PathValue("site_id")
	if siteID == "" {
		siteID = s.siteID
	}
	writeJSON(w, http.StatusOK, optimizer.BuildDayAheadPlan(time.Now(), s.store.Summary(siteID), optimizer.DefaultConfig()))
}

func (s *Server) customPlan(w http.ResponseWriter, r *http.Request) {
	siteID := r.PathValue("site_id")
	if siteID == "" {
		siteID = s.siteID
	}
	var cfg optimizer.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, optimizer.BuildDayAheadPlan(time.Now(), s.store.Summary(siteID), cfg))
}

func (s *Server) dispatchPreview(w http.ResponseWriter, r *http.Request) {
	siteID := r.PathValue("site_id")
	if siteID == "" {
		siteID = s.siteID
	}
	writeJSON(w, http.StatusOK, dispatch.BuildPreview(time.Now(), s.store.Summary(siteID), s.store.Devices(), optimizer.DefaultConfig()))
}

func (s *Server) customDispatchPreview(w http.ResponseWriter, r *http.Request) {
	siteID := r.PathValue("site_id")
	if siteID == "" {
		siteID = s.siteID
	}
	var cfg optimizer.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dispatch.BuildPreview(time.Now(), s.store.Summary(siteID), s.store.Devices(), cfg))
}

func (s *Server) applyDispatch(w http.ResponseWriter, r *http.Request) {
	if !s.requireControl(w, r) {
		return
	}
	siteID := r.PathValue("site_id")
	if siteID == "" {
		siteID = s.siteID
	}
	var req dispatch.ApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	preview := dispatch.BuildPreview(time.Now(), s.store.Summary(siteID), s.store.Devices(), req.Config)
	decision := dispatch.DecideApply(preview, req)
	if !decision.CanApply {
		writeJSON(w, http.StatusPreconditionFailed, decision)
		return
	}
	d, ok := s.store.Device(preview.CandidateDeviceID)
	if !ok {
		writeError(w, http.StatusNotFound, "candidate device not found")
		return
	}
	cmd := *preview.CandidateCommand
	cmd.CommandID = preview.CandidateDeviceID + "-" + time.Now().Format("20060102150405.000000000")
	cmd.IssuedAt = time.Now().Unix()
	cmd.Reason = "dispatch apply: " + cmd.Reason
	if err := s.publisher.PublishCommand(siteID, d, cmd); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"applied":  true,
		"reason":   decision.Reason,
		"device":   d,
		"command":  cmd,
		"preview":  preview,
		"decision": decision,
	})
}

func (s *Server) commands(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.Commands())
}

func (s *Server) events(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.Events())
}

func (s *Server) getPolicy(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.scheduler.Policy())
}

func (s *Server) setPolicy(w http.ResponseWriter, r *http.Request) {
	if !s.requireControl(w, r) {
		return
	}
	var p model.Policy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.scheduler.SetPolicy(p)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) command(w http.ResponseWriter, r *http.Request) {
	if !s.requireControl(w, r) {
		return
	}
	deviceID := r.PathValue("device_id")
	d, ok := s.store.Device(deviceID)
	if !ok {
		writeError(w, http.StatusNotFound, "device not found")
		return
	}
	var cmd model.Command
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(cmd.Action) == "" {
		writeError(w, http.StatusBadRequest, "action is required")
		return
	}
	cmd.CommandID = deviceID + "-" + time.Now().Format("20060102150405.000000000")
	cmd.IssuedAt = time.Now().Unix()
	if err := s.publisher.PublishCommand(d.SiteID, d, cmd); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, cmd)
}

func withJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			w.Header().Set("Content-Type", "application/json")
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *Server) requireControl(w http.ResponseWriter, r *http.Request) bool {
	if s.token == "" {
		return true
	}
	token := strings.TrimSpace(r.Header.Get("X-VPP-Control-Token"))
	if token == "" {
		token = bearerToken(r.Header.Get("Authorization"))
	}
	if token == s.token {
		return true
	}
	writeError(w, http.StatusUnauthorized, "control token required")
	return false
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}
