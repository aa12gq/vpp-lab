package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
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
	cacheRetention := getdur("EDGE_CACHE_RETENTION", 24*time.Hour)
	cleanupInterval := getdur("EDGE_CLEANUP_INTERVAL", time.Hour)
	httpAddr := getenv("EDGE_HTTP_ADDR", ":8081")
	captureKinds := parseKindSet(getenv("EDGE_CAPTURE_KINDS", "telemetry,event,status"))
	upstreamTopicPrefix := strings.Trim(getenv("EDGE_UPSTREAM_TOPIC_PREFIX", ""), "/")

	cache, err := edge.OpenCache(ctx, cachePath)
	if err != nil {
		log.Fatalf("open edge cache: %v", err)
	}
	defer cache.Close()
	if cacheRetention > 0 && cleanupInterval > 0 {
		go cleanupLoop(ctx, cache, cacheRetention, cleanupInterval)
		log.Printf("edge cache cleanup enabled retention=%s interval=%s", cacheRetention, cleanupInterval)
	} else {
		log.Printf("edge cache cleanup disabled retention=%s interval=%s", cacheRetention, cleanupInterval)
	}

	local := newMQTTClient(localBroker, getenv("EDGE_LOCAL_CLIENT_ID", "vpp-edge-gateway-local"), func(_ paho.Client, msg paho.Message) {
		parsed, ok := topic.Parse(msg.Topic())
		if !ok || !captureKinds[parsed.Kind] {
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
		go flushLoop(ctx, cache, upstream, flushInterval, upstreamTopicPrefix)
		log.Printf("edge gateway upstream enabled broker=%s interval=%s topic_prefix=%q", upstreamBroker, flushInterval, upstreamTopicPrefix)
	} else {
		log.Printf("edge gateway upstream disabled; messages stay cached")
	}

	httpSrv := &http.Server{
		Addr:              httpAddr,
		Handler:           edgeHTTPHandler(cache, local, upstream, cacheRetention),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("edge gateway http listening on %s", httpAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("edge http server: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}

func edgeHTTPHandler(cache *edge.Cache, local paho.Client, upstream paho.Client, cacheRetention time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		status := http.StatusOK
		body := map[string]interface{}{
			"status":              "ok",
			"local_mqtt":          local != nil && local.IsConnected(),
			"upstream_configured": upstream != nil,
			"upstream_mqtt":       upstream != nil && upstream.IsConnected(),
		}
		if local == nil || !local.IsConnected() {
			status = http.StatusServiceUnavailable
			body["status"] = "degraded"
		}
		writeJSON(w, status, body)
	})
	mux.HandleFunc("GET /api/v1/cache/stats", func(w http.ResponseWriter, r *http.Request) {
		stats, err := cache.Stats(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, stats)
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		stats, err := cache.Stats(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = fmt.Fprintf(w, "# HELP vpp_edge_cache_messages Edge MQTT cache messages by state.\n")
		_, _ = fmt.Fprintf(w, "# TYPE vpp_edge_cache_messages gauge\n")
		_, _ = fmt.Fprintf(w, "vpp_edge_cache_messages{state=\"pending\"} %d\n", stats.Pending)
		_, _ = fmt.Fprintf(w, "vpp_edge_cache_messages{state=\"total\"} %d\n", stats.Total)
		_, _ = fmt.Fprintf(w, "# HELP vpp_edge_cache_oldest_pending_age_seconds Age of the oldest pending edge cache message.\n")
		_, _ = fmt.Fprintf(w, "# TYPE vpp_edge_cache_oldest_pending_age_seconds gauge\n")
		_, _ = fmt.Fprintf(w, "vpp_edge_cache_oldest_pending_age_seconds %.0f\n", oldestPendingAgeSeconds(stats.OldestPendingAt))
		_, _ = fmt.Fprintf(w, "# HELP vpp_edge_cache_retention_seconds Edge cache retention for sent messages.\n")
		_, _ = fmt.Fprintf(w, "# TYPE vpp_edge_cache_retention_seconds gauge\n")
		_, _ = fmt.Fprintf(w, "vpp_edge_cache_retention_seconds %.0f\n", cacheRetention.Seconds())
		_, _ = fmt.Fprintf(w, "# HELP vpp_edge_mqtt_connected Edge MQTT connection state.\n")
		_, _ = fmt.Fprintf(w, "# TYPE vpp_edge_mqtt_connected gauge\n")
		_, _ = fmt.Fprintf(w, "vpp_edge_mqtt_connected{side=\"local\"} %.0f\n", boolFloat(local != nil && local.IsConnected()))
		_, _ = fmt.Fprintf(w, "vpp_edge_mqtt_connected{side=\"upstream\"} %.0f\n", boolFloat(upstream != nil && upstream.IsConnected()))
	})
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func boolFloat(ok bool) float64 {
	if ok {
		return 1
	}
	return 0
}

func oldestPendingAgeSeconds(at *time.Time) float64 {
	if at == nil {
		return 0
	}
	age := time.Since(*at)
	if age < 0 {
		return 0
	}
	return age.Seconds()
}

func cleanupLoop(ctx context.Context, cache *edge.Cache, retention time.Duration, interval time.Duration) {
	cleanupSent(ctx, cache, retention)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanupSent(ctx, cache, retention)
		}
	}
}

func cleanupSent(ctx context.Context, cache *edge.Cache, retention time.Duration) {
	deleted, err := cache.DeleteSentBefore(ctx, time.Now().UTC().Add(-retention))
	if err != nil {
		log.Printf("edge cache cleanup failed: %v", err)
		return
	}
	if deleted > 0 {
		log.Printf("edge cache cleanup deleted=%d", deleted)
	}
}

func flushLoop(ctx context.Context, cache *edge.Cache, upstream paho.Client, interval time.Duration, topicPrefix string) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			flushPending(ctx, cache, upstream, topicPrefix)
		}
	}
}

func flushPending(ctx context.Context, cache *edge.Cache, upstream paho.Client, topicPrefix string) {
	if upstream == nil || !upstream.IsConnected() {
		return
	}
	pending, err := cache.Pending(ctx, 100)
	if err != nil {
		log.Printf("load pending failed: %v", err)
		return
	}
	for _, msg := range pending {
		targetTopic := upstreamTopic(msg.Topic, topicPrefix)
		token := upstream.Publish(targetTopic, 1, false, msg.Payload)
		if token.Wait() && token.Error() != nil {
			log.Printf("forward failed id=%d topic=%s target=%s err=%v", msg.ID, msg.Topic, targetTopic, token.Error())
			return
		}
		if err := cache.MarkSent(ctx, msg.ID); err != nil {
			log.Printf("mark sent failed id=%d err=%v", msg.ID, err)
			return
		}
		log.Printf("forwarded id=%d topic=%s target=%s", msg.ID, msg.Topic, targetTopic)
	}
}

func parseKindSet(raw string) map[string]bool {
	out := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		kind := strings.TrimSpace(part)
		if kind == "" {
			continue
		}
		out[kind] = true
	}
	return out
}

func upstreamTopic(original string, prefix string) string {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return original
	}
	return prefix + "/" + strings.TrimLeft(original, "/")
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
