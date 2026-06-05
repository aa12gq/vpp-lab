package main

import (
	"encoding/json"
	"log"
	"math"
	"os"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"vpp-lab/internal/model"
	"vpp-lab/internal/topic"
)

func main() {
	broker := getenv("MQTT_BROKER", "tcp://localhost:1883")
	siteID := getenv("SITE_ID", "home-lab")
	client := paho.NewClient(paho.NewClientOptions().AddBroker(broker).SetClientID("vpp-simulator"))
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("connect mqtt: %v", token.Error())
	}
	defer client.Disconnect(250)

	var seq int64
	for range time.Tick(2 * time.Second) {
		seq++
		now := time.Now()
		h := float64(now.Hour()) + float64(now.Minute())/60
		pvPower := math.Max(0, 90*math.Sin((h-6)/12*math.Pi))
		load1 := 25 + 5*math.Sin(float64(seq)/10)
		load2 := 45 + 10*math.Sin(float64(seq)/7)
		soc := 0.55 + 0.2*math.Sin(float64(seq)/30)
		publish(client, siteID, model.DevicePV, "pv_01", seq, map[string]float64{"voltage": 18, "current": pvPower / 18, "power": pvPower})
		publish(client, siteID, model.DeviceBattery, "battery_01", seq, map[string]float64{"voltage": 12.4, "current": 0, "power": 0, "soc": soc, "temperature": 25})
		publish(client, siteID, model.DeviceLoad, "load_01", seq, map[string]float64{"voltage": 12, "current": load1 / 12, "power": load1})
		publish(client, siteID, model.DeviceLoad, "load_02", seq, map[string]float64{"voltage": 12, "current": load2 / 12, "power": load2})
		log.Printf("published seq=%d pv=%.1f load=%.1f soc=%.2f", seq, pvPower, load1+load2, soc)
	}
}

func publish(client paho.Client, siteID string, typ model.DeviceType, id string, seq int64, metrics map[string]float64) {
	payload, _ := json.Marshal(model.Telemetry{
		DeviceID:  id,
		Timestamp: time.Now().Unix(),
		Metrics:   metrics,
		State:     "online",
		Seq:       seq,
	})
	t := topic.Telemetry(siteID, string(typ), id)
	token := client.Publish(t, 1, false, payload)
	token.Wait()
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
