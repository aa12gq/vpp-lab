package deviceauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const Field = "auth"

type Auth struct {
	Timestamp int64  `json:"timestamp"`
	Signature string `json:"signature,omitempty"`
}

type Keys map[string]string

func ParseKeys(raw string) (Keys, error) {
	keys := make(Keys)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, secret, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("device key %q must use device_id=secret", part)
		}
		id = strings.TrimSpace(id)
		secret = strings.TrimSpace(secret)
		if id == "" || secret == "" {
			return nil, fmt.Errorf("device key %q has empty device id or secret", part)
		}
		keys[id] = secret
	}
	return keys, nil
}

func (k Keys) Enabled() bool {
	return len(k) > 0
}

func SignPayload(topic string, payload []byte, secret string, now time.Time) ([]byte, error) {
	msg, err := payloadMap(payload)
	if err != nil {
		return nil, err
	}
	auth := Auth{Timestamp: now.Unix()}
	msg[Field] = mustJSON(auth)
	canonical, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	auth.Signature = signature(topic, canonical, secret)
	msg[Field] = mustJSON(auth)
	return json.Marshal(msg)
}

func VerifyPayload(topic, deviceID string, payload []byte, keys Keys, now time.Time, maxSkew time.Duration) error {
	if !keys.Enabled() {
		return nil
	}
	secret, ok := keys[deviceID]
	if !ok {
		return fmt.Errorf("missing device key for %s", deviceID)
	}
	msg, err := payloadMap(payload)
	if err != nil {
		return err
	}
	rawAuth, ok := msg[Field]
	if !ok {
		return fmt.Errorf("missing auth")
	}
	var auth Auth
	if err := json.Unmarshal(rawAuth, &auth); err != nil {
		return fmt.Errorf("bad auth: %w", err)
	}
	if auth.Timestamp == 0 || auth.Signature == "" {
		return fmt.Errorf("auth timestamp and signature are required")
	}
	if maxSkew > 0 {
		ts := time.Unix(auth.Timestamp, 0)
		if now.Sub(ts) > maxSkew || ts.Sub(now) > maxSkew {
			return fmt.Errorf("auth timestamp outside max skew")
		}
	}
	expectedAuth := Auth{Timestamp: auth.Timestamp}
	msg[Field] = mustJSON(expectedAuth)
	canonical, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	expected := signature(topic, canonical, secret)
	if !hmac.Equal([]byte(expected), []byte(auth.Signature)) {
		return fmt.Errorf("bad signature")
	}
	return nil
}

func payloadMap(payload []byte) (map[string]json.RawMessage, error) {
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		return nil, fmt.Errorf("bad json payload: %w", err)
	}
	if msg == nil {
		return nil, fmt.Errorf("payload must be a json object")
	}
	return msg, nil
}

func signature(topic string, canonical []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(topic))
	_, _ = mac.Write([]byte{'\n'})
	_, _ = mac.Write(canonical)
	return hex.EncodeToString(mac.Sum(nil))
}

func mustJSON(v interface{}) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}
