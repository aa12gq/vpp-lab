package main

import (
	"encoding/json"
	"log"
	"math"
	"os"
	"strconv"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"vpp-lab/internal/deviceauth"
	"vpp-lab/internal/model"
	vppmqtt "vpp-lab/internal/mqtt"
	"vpp-lab/internal/topic"
)

func main() {
	broker := getenv("MQTT_BROKER", "tcp://localhost:1883")
	siteID := getenv("SITE_ID", "home-lab")
	commandTopic := "vpp/" + siteID + "/+/+/command"
	deviceKeys, err := deviceauth.ParseKeys(getenv("DEVICE_KEYS", ""))
	if err != nil {
		log.Fatalf("parse device keys: %v", err)
	}
	sim := newSimulator(siteID, deviceKeys)
	opts := paho.NewClientOptions().
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
		})
	if username := getenv("MQTT_USERNAME", ""); username != "" {
		opts.SetUsername(username).SetPassword(getenv("MQTT_PASSWORD", ""))
	}
	tlsConfig, err := vppmqtt.NewTLSConfig(vppmqtt.TLSFiles{
		CAFile:             getenv("MQTT_TLS_CA_FILE", ""),
		CertFile:           getenv("MQTT_TLS_CERT_FILE", ""),
		KeyFile:            getenv("MQTT_TLS_KEY_FILE", ""),
		InsecureSkipVerify: getbool("MQTT_TLS_INSECURE_SKIP_VERIFY", false),
	})
	if err != nil {
		log.Fatalf("load mqtt tls config: %v", err)
	}
	if tlsConfig != nil {
		opts.SetTLSConfig(tlsConfig)
	}
	client := paho.NewClient(opts)
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
		if !sim.relayOn("load_01") {
			load1 = 0
		}
		if !sim.relayOn("load_02") {
			load2 = 0
		}
		soc, batteryPower, batteryState := sim.batteryTelemetry()
		pvState := "on"
		if pvPower == 0 {
			pvState = "off"
		}
		publishState(client, siteID, model.DevicePV, "pv_01", seq, pvState, map[string]float64{"voltage": 18, "current": pvPower / 18, "power": pvPower}, deviceKeys)
		publishState(client, siteID, model.DeviceBattery, "battery_01", seq, batteryState, map[string]float64{"voltage": 12.4, "current": batteryPower / 12.4, "power": batteryPower, "soc": soc, "temperature": 25}, deviceKeys)
		sim.publishBatterySOCEvent(seq, soc)
		load1State := "on"
		if !sim.relayOn("load_01") {
			load1State = "off"
		}
		publishState(client, siteID, model.DeviceLoad, "load_01", seq, load1State, map[string]float64{"voltage": 12, "current": load1 / 12, "power": load1}, deviceKeys)
		load2State := "on"
		if !sim.relayOn("load_02") {
			load2State = "off"
		}
		publishState(client, siteID, model.DeviceLoad, "load_02", seq, load2State, map[string]float64{"voltage": 12, "current": load2 / 12, "power": load2}, deviceKeys)
		log.Printf("published seq=%d pv=%.1f load=%.1f soc=%.2f battery=%s", seq, pvPower, load1+load2, soc, batteryState)
	}
}

func publishState(client paho.Client, siteID string, typ model.DeviceType, id string, seq int64, state string, metrics map[string]float64, keys deviceauth.Keys) {
	payload, _ := json.Marshal(model.Telemetry{
		DeviceID:  id,
		Timestamp: time.Now().Unix(),
		Metrics:   metrics,
		State:     state,
		Seq:       seq,
	})
	t := topic.Telemetry(siteID, string(typ), id)
	payload = signPayload(t, id, payload, keys)
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

func getbool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func signPayload(topic string, deviceID string, payload []byte, keys deviceauth.Keys) []byte {
	if !keys.Enabled() {
		return payload
	}
	secret, ok := keys[deviceID]
	if !ok {
		return payload
	}
	signed, err := deviceauth.SignPayload(topic, payload, secret, time.Now())
	if err != nil {
		log.Printf("sign payload failed topic=%s device=%s err=%v", topic, deviceID, err)
		return payload
	}
	return signed
}

type simulator struct {
	mu          sync.RWMutex
	siteID      string
	client      paho.Client
	keys        deviceauth.Keys
	relays      map[string]bool
	batteryMode string
	soc         float64
	lowSOCOpen  bool
}

func newSimulator(siteID string, keys deviceauth.Keys) *simulator {
	return &simulator{
		siteID:      siteID,
		keys:        keys,
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
	case "charging":
		power = -15
		s.soc += 0.005
	case "discharging":
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

func (s *simulator) publishBatterySOCEvent(seq int64, soc float64) {
	if s.client == nil {
		return
	}
	s.mu.Lock()
	shouldWarn := soc <= 0.20 && !s.lowSOCOpen
	shouldRecover := soc >= 0.30 && s.lowSOCOpen
	if shouldWarn {
		s.lowSOCOpen = true
	}
	if shouldRecover {
		s.lowSOCOpen = false
	}
	s.mu.Unlock()
	switch {
	case shouldWarn:
		s.publishEvent("battery_01", "warning", "low_soc", "battery SOC is below 20%", seq, map[string]interface{}{"soc": soc})
	case shouldRecover:
		s.publishEvent("battery_01", "info", "soc_recovered", "battery SOC recovered above 30%", seq, map[string]interface{}{"soc": soc})
	}
}

func (s *simulator) publishEvent(deviceID, severity, code, message string, seq int64, details map[string]interface{}) {
	event := model.DeviceEvent{
		EventID:   deviceID + "-" + code + "-" + time.Now().Format("20060102150405"),
		Severity:  severity,
		Code:      code,
		Message:   message,
		Details:   details,
		Timestamp: time.Now().Unix(),
	}
	payload, _ := json.Marshal(event)
	t := "vpp/" + s.siteID + "/" + string(model.DeviceBattery) + "/" + deviceID + "/event"
	payload = signPayload(t, deviceID, payload, s.keys)
	token := s.client.Publish(t, 1, false, payload)
	if token.Wait() && token.Error() != nil {
		log.Printf("publish event failed topic=%s err=%v", t, token.Error())
		return
	}
	log.Printf("published event seq=%d device=%s severity=%s code=%s", seq, deviceID, severity, code)
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
		if mode != "charging" && mode != "discharging" && mode != "idle" {
			ack.OK = false
			ack.Error = "unsupported battery mode (use idle/charging/discharging)"
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
	payload = signPayload(ackTopic, parsed.DeviceID, payload, s.keys)
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
