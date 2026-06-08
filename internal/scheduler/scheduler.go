package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
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
	policyMu  sync.RWMutex
	policy    model.Policy
	sentMu    sync.Mutex
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
	if interval <= 0 {
		log.Printf("scheduler disabled: interval must be > 0, got %s", interval)
		return
	}
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
	policy := s.Policy()
	summary := s.store.Summary(s.siteID)
	devices := s.store.Devices()
	telemetry := s.store.TelemetryForSite(s.siteID)

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
			if soc < policy.BatteryMaxSOC {
				s.publish(ctx, b, "set_mode", "pv surplus: charge battery", map[string]interface{}{"mode": "charging"})
			}
		}
		return
	}

	if summary.NetPowerW > 10 {
		dispatched := false
		for _, b := range batteries {
			soc := telemetry[b.ID].Metrics["soc"]
			if soc > policy.BatteryMinSOC {
				if s.publish(ctx, b, "set_mode", "load deficit: discharge battery", map[string]interface{}{"mode": "discharging"}) {
					dispatched = true
				}
			}
		}
		if dispatched {
			return
		}
	}

	if summary.NetPowerW > policy.LoadShedThreshold {
		for _, load := range loads {
			if !load.CriticalLoad {
				s.publish(ctx, load, "set_relay", "load shed: non-critical load off", map[string]interface{}{"on": false})
				return
			}
		}
	}
}

func (s *Scheduler) Policy() model.Policy {
	s.policyMu.RLock()
	defer s.policyMu.RUnlock()
	return s.policy
}

func (s *Scheduler) SetPolicy(p model.Policy) {
	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	s.policy = p
}

func (s *Scheduler) publish(_ context.Context, d model.Device, action, reason string, params map[string]interface{}) bool {
	key := commandKey(d.ID, action, params)
	now := time.Now()
	s.sentMu.Lock()
	if last, ok := s.lastSent[key]; ok && time.Since(last) < s.cooldown {
		s.sentMu.Unlock()
		return false
	}
	s.lastSent[key] = now
	s.sentMu.Unlock()
	cmd := model.Command{
		CommandID: fmt.Sprintf("%s-%d", d.ID, now.UnixNano()),
		Action:    action,
		Params:    params,
		IssuedAt:  now.Unix(),
		Reason:    reason,
	}
	if err := s.commander.PublishCommand(s.siteID, d, cmd); err != nil {
		s.sentMu.Lock()
		if s.lastSent[key].Equal(now) {
			delete(s.lastSent, key)
		}
		s.sentMu.Unlock()
		log.Printf("publish command failed device=%s action=%s err=%v", d.ID, action, err)
		return false
	}
	log.Printf("scheduler command device=%s action=%s reason=%q", d.ID, action, reason)
	return true
}

func ValidatePolicy(p model.Policy) error {
	if p.BatteryMinSOC < 0 || p.BatteryMinSOC > 1 {
		return fmt.Errorf("battery_min_soc must be between 0 and 1")
	}
	if p.BatteryMaxSOC < 0 || p.BatteryMaxSOC > 1 {
		return fmt.Errorf("battery_max_soc must be between 0 and 1")
	}
	if p.BatteryMinSOC > p.BatteryMaxSOC {
		return fmt.Errorf("battery_min_soc must be <= battery_max_soc")
	}
	if p.LoadShedThreshold < 0 {
		return fmt.Errorf("load_shed_threshold_w must be >= 0")
	}
	return nil
}

func commandKey(deviceID, action string, params map[string]interface{}) string {
	payload, _ := json.Marshal(params)
	return deviceID + ":" + action + ":" + string(payload)
}
