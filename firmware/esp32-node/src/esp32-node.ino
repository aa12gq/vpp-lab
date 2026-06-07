#include <ArduinoJson.h>
#include <PubSubClient.h>
#include <WiFi.h>
#include <Wire.h>
#include <time.h>

/*
  VPP Lab ESP32 load node.

  Arduino Library Manager dependencies:
  - PubSubClient
  - ArduinoJson
  - Adafruit INA219, optional when USE_INA219 is true

  Hardware defaults:
  - Relay IN: GPIO5
  - Status LED: GPIO2
  - I2C SDA/SCL: GPIO21/GPIO22
*/

// ---------- Required local config ----------
const char *wifiSsid = "YOUR_WIFI";
const char *wifiPassword = "YOUR_PASSWORD";
const char *mqttHost = "192.168.1.10";
const int mqttPort = 1883;
const char *mqttUsername = "";
const char *mqttPassword = "";

const char *siteId = "home-lab";
const char *deviceType = "load";
const char *deviceId = "load_02";

// ---------- Hardware config ----------
const int relayPin = 5;
const int ledPin = 2;
const bool relayActiveHigh = true;
const float fallbackVoltageV = 12.0;
const float fallbackCurrentA = 1.2;

// Set to 1 after wiring INA219/INA226 and adding the matching library/read function.
#define USE_INA219 0

#if USE_INA219
#include <Adafruit_INA219.h>
Adafruit_INA219 ina219;
#endif

WiFiClient wifiClient;
PubSubClient mqtt(wifiClient);

uint64_t seq = 0;
bool relayOn = false;
unsigned long lastPublishMs = 0;

String topicTelemetry() {
  return String("vpp/") + siteId + "/" + deviceType + "/" + deviceId + "/telemetry";
}

String topicCommand() {
  return String("vpp/") + siteId + "/" + deviceType + "/" + deviceId + "/command";
}

String topicAck() {
  return String("vpp/") + siteId + "/" + deviceType + "/" + deviceId + "/command/ack";
}

void setup() {
  pinMode(relayPin, OUTPUT);
  pinMode(ledPin, OUTPUT);
  setRelay(false);

  Serial.begin(115200);
  delay(200);

  Wire.begin(21, 22);
#if USE_INA219
  if (!ina219.begin()) {
    Serial.println("INA219 not found; telemetry will use fallback values");
  }
#endif

  connectWiFi();
  syncClock();

  mqtt.setServer(mqttHost, mqttPort);
  mqtt.setCallback(onMessage);
  mqtt.setBufferSize(768);
}

void loop() {
  if (WiFi.status() != WL_CONNECTED) {
    connectWiFi();
  }
  if (!mqtt.connected()) {
    reconnectMQTT();
  }
  mqtt.loop();

  if (millis() - lastPublishMs >= 2000) {
    lastPublishMs = millis();
    publishTelemetry();
  }
}

void connectWiFi() {
  WiFi.mode(WIFI_STA);
  WiFi.begin(wifiSsid, wifiPassword);
  Serial.print("connecting wifi");
  while (WiFi.status() != WL_CONNECTED) {
    delay(500);
    Serial.print(".");
  }
  Serial.printf("\nwifi connected ip=%s\n", WiFi.localIP().toString().c_str());
}

void syncClock() {
  configTime(8 * 3600, 0, "pool.ntp.org", "time.nist.gov");
  time_t now = time(nullptr);
  unsigned long start = millis();
  while (now < 1700000000 && millis() - start < 8000) {
    delay(250);
    now = time(nullptr);
  }
  Serial.printf("clock unix=%ld\n", long(now));
}

void reconnectMQTT() {
  while (!mqtt.connected()) {
    String clientId = String("esp32-") + deviceId;
    bool ok;
    if (strlen(mqttUsername) > 0) {
      ok = mqtt.connect(clientId.c_str(), mqttUsername, mqttPassword);
    } else {
      ok = mqtt.connect(clientId.c_str());
    }
    if (ok) {
      mqtt.subscribe(topicCommand().c_str(), 1);
      publishStatus("online");
      Serial.printf("mqtt connected command_topic=%s\n", topicCommand().c_str());
    } else {
      Serial.printf("mqtt connect failed rc=%d\n", mqtt.state());
      delay(1000);
    }
  }
}

void publishTelemetry() {
  seq++;

  float voltage = fallbackVoltageV;
  float current = relayOn ? fallbackCurrentA : 0.0;

#if USE_INA219
  voltage = ina219.getBusVoltage_V();
  current = ina219.getCurrent_mA() / 1000.0;
#endif

  StaticJsonDocument<512> doc;
  doc["device_id"] = deviceId;
  doc["timestamp"] = nowUnix();
  doc["state"] = relayOn ? "on" : "off";
  doc["seq"] = seq;

  JsonObject metrics = doc.createNestedObject("metrics");
  metrics["voltage"] = voltage;
  metrics["current"] = current;
  metrics["power"] = voltage * current;

  publishJSON(topicTelemetry(), doc);
}

void publishStatus(const char *state) {
  StaticJsonDocument<256> doc;
  doc["device_id"] = deviceId;
  doc["timestamp"] = nowUnix();
  doc["state"] = state;
  doc["seq"] = seq;
  publishJSON(String("vpp/") + siteId + "/" + deviceType + "/" + deviceId + "/status", doc);
}

void onMessage(char *rawTopic, byte *payload, unsigned int length) {
  StaticJsonDocument<384> doc;
  DeserializationError err = deserializeJson(doc, payload, length);
  if (err) {
    publishAck("", false, "bad json");
    return;
  }

  const char *commandId = doc["command_id"] | "";
  const char *action = doc["action"] | "";

  if (strcmp(action, "set_relay") == 0) {
    bool on = doc["params"]["on"] | false;
    setRelay(on);
    publishAck(commandId, true, "");
    publishTelemetry();
    return;
  }

  publishAck(commandId, false, "unsupported action");
}

void publishAck(const char *commandId, bool ok, const char *error) {
  StaticJsonDocument<256> ack;
  ack["command_id"] = commandId;
  ack["ok"] = ok;
  if (!ok && strlen(error) > 0) {
    ack["error"] = error;
  }
  ack["timestamp"] = nowUnix();
  publishJSON(topicAck(), ack);
}

void publishJSON(const String &topic, JsonDocument &doc) {
  char payload[768];
  size_t n = serializeJson(doc, payload, sizeof(payload));
  if (n == 0 || n >= sizeof(payload)) {
    Serial.println("json payload too large");
    return;
  }
  bool ok = mqtt.publish(topic.c_str(), payload, false);
  if (!ok) {
    Serial.printf("publish failed topic=%s\n", topic.c_str());
  }
}

void setRelay(bool on) {
  relayOn = on;
  digitalWrite(relayPin, relayActiveHigh ? (on ? HIGH : LOW) : (on ? LOW : HIGH));
  digitalWrite(ledPin, on ? HIGH : LOW);
}

long nowUnix() {
  time_t now = time(nullptr);
  if (now > 1700000000) {
    return long(now);
  }
  return long(millis() / 1000);
}
