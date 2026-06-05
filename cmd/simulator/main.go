package main

import (
	"encoding/json"
	"log"
	"math"
	"os"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"vpp-lab/internal/model"
	"vpp-lab/internal/topic"
)

func main() {
	broker := getenv("MQTT_BROKER", "tcp://localhost:1883")
	siteID := getenv("SITE_ID", "home-lab")
	commandTopic := "vpp/" + siteID + "/+/+/command"
	sim := newSimulator(siteID)
	client := paho.NewClient(paho.NewClientOptions().
		AddBroker(broker).
		SetClientID("vpp-simulator").
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(2 * time.Second).
		SetDefaultPublishHandler(sim.handleCommand).
		SetOnConnectHandler(func(client paho.Client) {
			if token := client.Subscribe(commandTopic, 1, sim.handleCommand); token.Wait() && token.Error() != nil {
				log.Printf("subscribe command failed: %v", token.Error())
				return
			}
			log.Printf("simulator subscribed command topic: %s", commandTopic)
		}))
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("connect mqtt: %v", token.Error())
	}
	defer client.Disconnect(250)
	sim.client = client

	var seq int64
	for range time.Tick(2 * time.Second) {
		seq++
		now := time.Now()
		h := float64(now.Hour()) + float64(now.Minute())/60
		pvPower := math.Max(0, 90*math.Sin((h-6)/12*math.Pi))
		load1 := 25 + 5*math.Sin(float64(seq)/10)
		load2 := 45 + 10*math.Sin(float64(seq)/7)
		if !sim.relayOn("load_02") {
			load2 = 0
		}
		soc, batteryPower, batteryState := sim.batteryTelemetry()
		publish(client, siteID, model.DevicePV, "pv_01", seq, map[string]float64{"voltage": 18, "current": pvPower / 18, "power": pvPower})
		publishState(client, siteID, model.DeviceBattery, "battery_01", seq, batteryState, map[string]float64{"voltage": 12.4, "current": batteryPower / 12.4, "power": batteryPower, "soc": soc, "temperature": 25})
		publish(client, siteID, model.DeviceLoad, "load_01", seq, map[string]float64{"voltage": 12, "current": load1 / 12, "power": load1})
		publish(client, siteID, model.DeviceLoad, "load_02", seq, map[string]float64{"voltage": 12, "current": load2 / 12, "power": load2})
		log.Printf("published seq=%d pv=%.1f load=%.1f soc=%.2f battery=%s", seq, pvPower, load1+load2, soc, batteryState)
	}
}

func publish(client paho.Client, siteID string, typ model.DeviceType, id string, seq int64, metrics map[string]float64) {
	publishState(client, siteID, typ, id, seq, "online", metrics)
}

func publishState(client paho.Client, siteID string, typ model.DeviceType, id string, seq int64, state string, metrics map[string]float64) {
	payload, _ := json.Marshal(model.Telemetry{
		DeviceID:  id,
		Timestamp: time.Now().Unix(),
		Metrics:   metrics,
		State:     state,
		Seq:       seq,
	})
	t := topic.Telemetry(siteID, string(typ), id)
	token := client.Publish(t, 1, false, payload)
	if token.Wait() && token.Error() != nil {
		log.Printf("publish telemetry failed topic=%s err=%v", t, token.Error())
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type simulator struct {
	mu          sync.RWMutex
	siteID      string
	client      paho.Client
	relays      map[string]bool
	batteryMode string
	soc         float64
}

func newSimulator(siteID string) *simulator {
	return &simulator{
		siteID:      siteID,
		relays:      map[string]bool{"load_01": true, "load_02": true},
		batteryMode: "idle",
		soc:         0.65,
	}
}

func (s *simulator) relayOn(deviceID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.relays[deviceID]
}

func (s *simulator) batteryTelemetry() (float64, float64, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	power := 0.0
	switch s.batteryMode {
	case "charge":
		power = -15
		s.soc += 0.005
	case "discharge":
		power = 15
		s.soc -= 0.005
	default:
		power = 0
	}
	if s.soc >= 0.95 {
		s.soc = 0.95
		s.batteryMode = "idle"
	}
	if s.soc <= 0.15 {
		s.soc = 0.15
		s.batteryMode = "idle"
	}
	return s.soc, power, s.batteryMode
}

func (s *simulator) handleCommand(_ paho.Client, msg paho.Message) {
	parsed, ok := topic.Parse(msg.Topic())
	if !ok || parsed.Kind != "command" {
		return
	}
	var cmd model.Command
	if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
		log.Printf("bad command topic=%s err=%v", msg.Topic(), err)
		return
	}

	ack := model.CommandAck{
		CommandID: cmd.CommandID,
		OK:        true,
		Timestamp: time.Now().Unix(),
	}
	s.mu.Lock()
	switch cmd.Action {
	case "set_relay":
		on, ok := boolParam(cmd.Params, "on")
		if !ok || parsed.DeviceType != string(model.DeviceLoad) {
			ack.OK = false
			ack.Error = "set_relay requires load device and bool params.on"
			break
		}
		s.relays[parsed.DeviceID] = on
	case "set_mode":
		mode, ok := stringParam(cmd.Params, "mode")
		if !ok || parsed.DeviceType != string(model.DeviceBattery) {
			ack.OK = false
			ack.Error = "set_mode requires battery device and string params.mode"
			break
		}
		if mode != "charge" && mode != "discharge" && mode != "idle" {
			ack.OK = false
			ack.Error = "unsupported battery mode"
			break
		}
		s.batteryMode = mode
	default:
		ack.OK = false
		ack.Error = "unsupported action"
	}
	s.mu.Unlock()

	payload, _ := json.Marshal(ack)
	ackTopic := "vpp/" + s.siteID + "/" + parsed.DeviceType + "/" + parsed.DeviceID + "/command/ack"
	token := s.client.Publish(ackTopic, 1, false, payload)
	token.Wait()
	log.Printf("command device=%s action=%s ok=%v error=%s", parsed.DeviceID, cmd.Action, ack.OK, ack.Error)
}

func boolParam(params map[string]interface{}, key string) (bool, bool) {
	v, ok := params[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func stringParam(params map[string]interface{}, key string) (string, bool) {
	v, ok := params[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
