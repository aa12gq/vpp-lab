package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"vpp-lab/internal/model"
	"vpp-lab/internal/state"
)

type Commander interface {
	PublishCommand(siteID string, d model.Device, cmd model.Command) error
}

type Scheduler struct {
	siteID    string
	store     *state.Store
	commander Commander
	policy    model.Policy
	lastSent  map[string]time.Time
	cooldown  time.Duration
}

func New(siteID string, store *state.Store, commander Commander, policy model.Policy) *Scheduler {
	return &Scheduler{
		siteID:    siteID,
		store:     store,
		commander: commander,
		policy:    policy,
		lastSent:  make(map[string]time.Time),
		cooldown:  30 * time.Second,
	}
}

func (s *Scheduler) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Tick(ctx)
		}
	}
}

func (s *Scheduler) Tick(ctx context.Context) {
	summary := s.store.Summary(s.siteID)
	devices := s.store.Devices()
	telemetry := s.store.Telemetry()

	var batteries []model.Device
	var loads []model.Device
	for _, d := range devices {
		if d.SiteID != s.siteID {
			continue
		}
		switch d.Type {
		case model.DeviceBattery:
			batteries = append(batteries, d)
		case model.DeviceLoad:
			loads = append(loads, d)
		}
	}

	if summary.NetPowerW < -10 {
		for _, b := range batteries {
			soc := telemetry[b.ID].Metrics["soc"]
			if soc < s.policy.BatteryMaxSOC {
				s.publish(ctx, b, "set_mode", "pv surplus: charge battery", map[string]interface{}{"mode": "charging"})
			}
		}
		return
	}

	if summary.NetPowerW > 10 {
		for _, b := range batteries {
			soc := telemetry[b.ID].Metrics["soc"]
			if soc > s.policy.BatteryMinSOC {
				s.publish(ctx, b, "set_mode", "load deficit: discharge battery", map[string]interface{}{"mode": "discharging"})
				return
			}
		}
	}

	if summary.NetPowerW > s.policy.LoadShedThreshold {
		for _, load := range loads {
			if !load.CriticalLoad {
				s.publish(ctx, load, "set_relay", "load shed: non-critical load off", map[string]interface{}{"on": false})
				return
			}
		}
	}
}

func (s *Scheduler) Policy() model.Policy {
	return s.policy
}

func (s *Scheduler) SetPolicy(p model.Policy) {
	s.policy = p
}

func (s *Scheduler) publish(_ context.Context, d model.Device, action, reason string, params map[string]interface{}) {
	key := commandKey(d.ID, action, params)
	if last, ok := s.lastSent[key]; ok && time.Since(last) < s.cooldown {
		return
	}
	cmd := model.Command{
		CommandID: fmt.Sprintf("%s-%d", d.ID, time.Now().UnixNano()),
		Action:    action,
		Params:    params,
		IssuedAt:  time.Now().Unix(),
		Reason:    reason,
	}
	if err := s.commander.PublishCommand(s.siteID, d, cmd); err != nil {
		log.Printf("publish command failed device=%s action=%s err=%v", d.ID, action, err)
		return
	}
	s.lastSent[key] = time.Now()
	log.Printf("scheduler command device=%s action=%s reason=%q", d.ID, action, reason)
}

func commandKey(deviceID, action string, params map[string]interface{}) string {
	payload, _ := json.Marshal(params)
	return deviceID + ":" + action + ":" + string(payload)
}
