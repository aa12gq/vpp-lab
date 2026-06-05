package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"vpp-lab/internal/edge"
	"vpp-lab/internal/topic"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	siteID := getenv("SITE_ID", "home-lab")
	localBroker := getenv("EDGE_LOCAL_BROKER", getenv("MQTT_BROKER", "tcp://localhost:1883"))
	upstreamBroker := getenv("EDGE_UPSTREAM_BROKER", "")
	cachePath := getenv("EDGE_CACHE_PATH", "./data/edge-gateway/cache.db")
	flushInterval := getdur("EDGE_FLUSH_INTERVAL", 5*time.Second)

	cache, err := edge.OpenCache(ctx, cachePath)
	if err != nil {
		log.Fatalf("open edge cache: %v", err)
	}
	defer cache.Close()

	local := newMQTTClient(localBroker, getenv("EDGE_LOCAL_CLIENT_ID", "vpp-edge-gateway-local"), func(_ paho.Client, msg paho.Message) {
		if _, ok := topic.Parse(msg.Topic()); !ok {
			return
		}
		payload := append([]byte(nil), msg.Payload()...)
		if err := cache.Put(context.Background(), msg.Topic(), payload); err != nil {
			log.Printf("cache message failed topic=%s err=%v", msg.Topic(), err)
			return
		}
		log.Printf("cached topic=%s bytes=%d", msg.Topic(), len(payload))
	})
	if err := connect(local); err != nil {
		log.Fatalf("connect local mqtt: %v", err)
	}
	defer local.Disconnect(250)

	subTopic := topic.Wildcard(siteID)
	if token := local.Subscribe(subTopic, 1, nil); token.Wait() && token.Error() != nil {
		log.Fatalf("subscribe local mqtt: %v", token.Error())
	}
	log.Printf("edge gateway subscribed local=%s topic=%s cache=%s", localBroker, subTopic, cachePath)

	var upstream paho.Client
	if upstreamBroker != "" {
		upstream = newMQTTClient(upstreamBroker, getenv("EDGE_UPSTREAM_CLIENT_ID", "vpp-edge-gateway-upstream"), nil)
		if err := connect(upstream); err != nil {
			log.Fatalf("connect upstream mqtt: %v", err)
		}
		defer upstream.Disconnect(250)
		go flushLoop(ctx, cache, upstream, flushInterval)
		log.Printf("edge gateway upstream enabled broker=%s interval=%s", upstreamBroker, flushInterval)
	} else {
		log.Printf("edge gateway upstream disabled; messages stay cached")
	}

	<-ctx.Done()
}

func flushLoop(ctx context.Context, cache *edge.Cache, upstream paho.Client, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			flushPending(ctx, cache, upstream)
		}
	}
}

func flushPending(ctx context.Context, cache *edge.Cache, upstream paho.Client) {
	if upstream == nil || !upstream.IsConnected() {
		return
	}
	pending, err := cache.Pending(ctx, 100)
	if err != nil {
		log.Printf("load pending failed: %v", err)
		return
	}
	for _, msg := range pending {
		token := upstream.Publish(msg.Topic, 1, false, msg.Payload)
		if token.Wait() && token.Error() != nil {
			log.Printf("forward failed id=%d topic=%s err=%v", msg.ID, msg.Topic, token.Error())
			return
		}
		if err := cache.MarkSent(ctx, msg.ID); err != nil {
			log.Printf("mark sent failed id=%d err=%v", msg.ID, err)
			return
		}
		log.Printf("forwarded id=%d topic=%s", msg.ID, msg.Topic)
	}
}

func newMQTTClient(broker, clientID string, handler paho.MessageHandler) paho.Client {
	opts := paho.NewClientOptions().
		AddBroker(broker).
		SetClientID(clientID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(2 * time.Second)
	if handler != nil {
		opts.SetDefaultPublishHandler(handler)
	}
	return paho.NewClient(opts)
}

func connect(client paho.Client) error {
	token := client.Connect()
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getdur(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err == nil {
		return d
	}
	i, err := strconv.Atoi(v)
	if err == nil {
		return time.Duration(i) * time.Second
	}
	return fallback
}
