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
	cfg      config.Config
	client   paho.Client
	state    *state.Store
	ts       *timeseries.Writer
	recorder CommandRecorder
}

type CommandRecorder interface {
	PutCommandIssued(ctx context.Context, siteID string, d model.Device, cmd model.Command) error
	PutCommandAck(ctx context.Context, siteID string, deviceType model.DeviceType, deviceID string, ack model.CommandAck) error
	PutEvent(ctx context.Context, event model.DeviceEvent) error
}

func NewClient(cfg config.Config, store *state.Store, ts *timeseries.Writer) *Client {
	return &Client{cfg: cfg, state: store, ts: ts}
}

func (c *Client) WithCommandRecorder(recorder CommandRecorder) *Client {
	c.recorder = recorder
	return c
}

func (c *Client) Connect(ctx context.Context) error {
	opts := paho.NewClientOptions().
		AddBroker(c.cfg.MQTTBroker).
		SetClientID(c.cfg.MQTTClientID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(2 * time.Second).
		SetOnConnectHandler(func(client paho.Client) {
			topics := map[string]byte{
				topic.Wildcard(c.cfg.SiteID):           1,
				topic.CommandAckWildcard(c.cfg.SiteID): 1,
			}
			if token := client.SubscribeMultiple(topics, c.handleMessage); token.Wait() && token.Error() != nil {
				log.Printf("mqtt subscribe failed: %v", token.Error())
				return
			}
			log.Printf("mqtt subscribed: %v", topics)
		})
	if c.cfg.MQTTUsername != "" {
		opts.SetUsername(c.cfg.MQTTUsername).SetPassword(c.cfg.MQTTPassword)
	}
	tlsConfig, err := NewTLSConfig(TLSFiles{
		CAFile:             c.cfg.MQTTTLSCAFile,
		CertFile:           c.cfg.MQTTTLSCertFile,
		KeyFile:            c.cfg.MQTTTLSKeyFile,
		InsecureSkipVerify: c.cfg.MQTTTLSInsecure,
	})
	if err != nil {
		return err
	}
	if tlsConfig != nil {
		opts.SetTLSConfig(tlsConfig)
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
	c.state.PutCommandIssued(siteID, d, cmd)
	if c.recorder != nil {
		if err := c.recorder.PutCommandIssued(context.Background(), siteID, d, cmd); err != nil {
			log.Printf("persist command issued failed command=%s err=%v", cmd.CommandID, err)
		}
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
		var ack model.CommandAck
		if err := json.Unmarshal(msg.Payload(), &ack); err != nil {
			log.Printf("bad command ack topic=%s err=%v", msg.Topic(), err)
			return
		}
		c.state.PutCommandAck(parsed.DeviceID, ack)
		if c.recorder != nil {
			if err := c.recorder.PutCommandAck(context.Background(), parsed.SiteID, model.DeviceType(parsed.DeviceType), parsed.DeviceID, ack); err != nil {
				log.Printf("persist command ack failed command=%s err=%v", ack.CommandID, err)
			}
		}
		log.Printf("command ack topic=%s payload=%s", msg.Topic(), string(msg.Payload()))
	case "command":
		return
	case "event":
		var event model.DeviceEvent
		if err := json.Unmarshal(msg.Payload(), &event); err != nil {
			log.Printf("bad event topic=%s err=%v", msg.Topic(), err)
			return
		}
		event.SiteID = parsed.SiteID
		event.DeviceID = parsed.DeviceID
		event.DeviceType = model.DeviceType(parsed.DeviceType)
		if event.Timestamp == 0 {
			event.Timestamp = time.Now().Unix()
		}
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now()
		}
		if event.EventID == "" {
			event.EventID = fmt.Sprintf("%s-%d", event.DeviceID, event.Timestamp)
		}
		c.state.PutEvent(event)
		if c.recorder != nil {
			if err := c.recorder.PutEvent(context.Background(), event); err != nil {
				log.Printf("persist event failed event=%s err=%v", event.EventID, err)
			}
		}
		log.Printf("device event topic=%s severity=%s code=%s message=%s", msg.Topic(), event.Severity, event.Code, event.Message)
	case "status":
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
