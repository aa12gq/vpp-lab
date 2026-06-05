package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"vpp-lab/internal/config"
	"vpp-lab/internal/model"
	"vpp-lab/internal/state"
	"vpp-lab/internal/timeseries"
	"vpp-lab/internal/topic"
)

type Client struct {
	cfg    config.Config
	client paho.Client
	state  *state.Store
	ts     *timeseries.Writer
}

func NewClient(cfg config.Config, store *state.Store, ts *timeseries.Writer) *Client {
	return &Client{cfg: cfg, state: store, ts: ts}
}

func (c *Client) Connect(ctx context.Context) error {
	opts := paho.NewClientOptions().
		AddBroker(c.cfg.MQTTBroker).
		SetClientID(c.cfg.MQTTClientID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(2 * time.Second).
		SetOnConnectHandler(func(client paho.Client) {
			t := topic.Wildcard(c.cfg.SiteID)
			if token := client.Subscribe(t, 1, c.handleMessage); token.Wait() && token.Error() != nil {
				log.Printf("mqtt subscribe failed: %v", token.Error())
			} else {
				log.Printf("mqtt subscribed: %s", t)
			}
		})
	if c.cfg.MQTTUsername != "" {
		opts.SetUsername(c.cfg.MQTTUsername).SetPassword(c.cfg.MQTTPassword)
	}
	c.client = paho.NewClient(opts)
	token := c.client.Connect()
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	go func() {
		<-ctx.Done()
		c.client.Disconnect(250)
	}()
	return nil
}

func (c *Client) PublishCommand(siteID string, d model.Device, cmd model.Command) error {
	payload, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	t := topic.Command(siteID, string(d.Type), d.ID)
	token := c.client.Publish(t, 1, false, payload)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return nil
}

func (c *Client) handleMessage(_ paho.Client, msg paho.Message) {
	parsed, ok := topic.Parse(msg.Topic())
	if !ok {
		return
	}
	switch parsed.Kind {
	case "telemetry":
		var tele model.Telemetry
		if err := json.Unmarshal(msg.Payload(), &tele); err != nil {
			log.Printf("bad telemetry topic=%s err=%v", msg.Topic(), err)
			return
		}
		tele.SiteID = parsed.SiteID
		tele.DeviceID = parsed.DeviceID
		tele.Type = model.DeviceType(parsed.DeviceType)
		if tele.Timestamp == 0 {
			tele.Timestamp = time.Now().Unix()
		}
		c.state.PutTelemetry(tele)
		if c.ts != nil {
			if err := c.ts.WriteTelemetry(context.Background(), tele); err != nil {
				log.Printf("write influx failed device=%s err=%v", tele.DeviceID, err)
			}
		}
	case "command/ack":
		log.Printf("command ack topic=%s payload=%s", msg.Topic(), string(msg.Payload()))
	case "status", "event":
		log.Printf("device %s topic=%s payload=%s", parsed.DeviceID, parsed.Kind, string(msg.Payload()))
	default:
		log.Printf("ignored mqtt topic=%s", msg.Topic())
	}
}

func (c *Client) Healthy() error {
	if c.client == nil || !c.client.IsConnected() {
		return fmt.Errorf("mqtt disconnected")
	}
	return nil
}
