/*
  VPP Lab — ESP32 PV Node Firmware.
  Replaces simulator pv_01/pv_02 with real INA226 power monitoring.

  Arduino Library Manager dependencies:
    - PubSubClient
    - ArduinoJson

  Hardware:
    - INA226 on I2C (SDA=GPIO21, SCL=GPIO22), addr 0x40
    - Status LED on GPIO2
    - (Optional) Curtailment MOSFET on GPIO5

  Dev note:
    PlatformIO: this include works as-is.
    Arduino IDE: copy ../esp32-common/vpp_common.h into this folder,
    then change to #include "vpp_common.h".
*/
#include "../../esp32-common/vpp_common.h"

#include <PubSubClient.h>
#include <WiFi.h>
#include <time.h>

// ═══════════════════════════════════════════
// User config — edit these per deployment
// ═══════════════════════════════════════════

const char *wifiSsid      = "YOUR_WIFI";
const char *wifiPassword  = "YOUR_PASSWORD";
const char *mqttHost      = "192.168.1.10";
const int   mqttPort      = 1883;
const char *mqttUsername  = "";
const char *mqttPassword  = "";

const char *siteId        = "home-lab";
const char *deviceType    = "pv";
const char *deviceId      = "pv_01";

// ═══════════════════════════════════════════
// Auth config — set DEVICE_SECRET to enable HMAC signing
// ═══════════════════════════════════════════

// #define DEVICE_SECRET "your-device-secret"

// ═══════════════════════════════════════════
// Hardware config
// ═══════════════════════════════════════════

const int ledPin           = 2;
const int curtailPin       = 5;    // MOSFET for PV curtailment (optional)

// ═══════════════════════════════════════════
// Globals
// ═══════════════════════════════════════════

WiFiClient   wifiClient;
PubSubClient mqtt(wifiClient);
INA226       ina;

uint64_t      seq         = 0;
bool          curtailed   = false;  // true = PV disconnected/curtailed
unsigned long lastPubMs   = 0;

// ═══════════════════════════════════════════
// Topic helpers
// ═══════════════════════════════════════════

String topicTelemetry() { return mqttTopicTelemetry(siteId, deviceType, deviceId); }
String topicCommand()   { return mqttTopicCommand(siteId, deviceType, deviceId); }
String topicAck()       { return mqttTopicAck(siteId, deviceType, deviceId); }
String topicStatus()    { return mqttTopicStatus(siteId, deviceType, deviceId); }

// ═══════════════════════════════════════════
// Setup
// ═══════════════════════════════════════════

void setup() {
  pinMode(ledPin, OUTPUT);
  pinMode(curtailPin, OUTPUT);
  setCurtail(false);

  Serial.begin(115200);
  delay(200);

  Wire.begin(21, 22);
  if (!ina.begin()) {
    Serial.println("INA226 not found — fallback to zero readings");
  } else {
    Serial.println("INA226 detected");
  }

  connectWiFi();
  syncClock();

  mqtt.setServer(mqttHost, mqttPort);
  mqtt.setCallback(onMessage);
  mqtt.setBufferSize(1024);
}

// ═══════════════════════════════════════════
// Main loop
// ═══════════════════════════════════════════

void loop() {
  if (WiFi.status() != WL_CONNECTED) connectWiFi();
  if (!mqtt.connected()) reconnectMQTT();
  mqtt.loop();

  if (millis() - lastPubMs >= 2000) {
    lastPubMs = millis();
    publishTelemetry();
  }
}

// ═══════════════════════════════════════════
// WiFi
// ═══════════════════════════════════════════

void connectWiFi() {
  WiFi.mode(WIFI_STA);
  WiFi.begin(wifiSsid, wifiPassword);
  Serial.print("connecting wifi");
  int tries = 0;
  while (WiFi.status() != WL_CONNECTED && tries < 40) {
    delay(500);
    Serial.print(".");
    tries++;
  }
  if (WiFi.status() == WL_CONNECTED) {
    Serial.printf("\nwifi connected ip=%s\n", WiFi.localIP().toString().c_str());
  } else {
    Serial.println("\nwifi failed — will retry");
  }
}

// ═══════════════════════════════════════════
// NTP clock sync
// ═══════════════════════════════════════════

void syncClock() {
  configTime(8 * 3600, 0, "pool.ntp.org", "time.nist.gov");
  time_t now = time(nullptr);
  unsigned long start = millis();
  while (now < 1700000000 && millis() - start < 10000) {
    delay(250);
    now = time(nullptr);
  }
  Serial.printf("clock unix=%ld\n", long(now));
}

// ═══════════════════════════════════════════
// MQTT
// ═══════════════════════════════════════════

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
      Serial.printf("mqtt connected cmd_topic=%s\n", topicCommand().c_str());
    } else {
      Serial.printf("mqtt connect failed rc=%d\n", mqtt.state());
      delay(1000);
    }
  }
}

// ═══════════════════════════════════════════
// Telemetry
// ═══════════════════════════════════════════

void publishTelemetry() {
  seq++;

  float voltage = 0.0;
  float current = 0.0;
  float power   = 0.0;

  if (ina.begin()) {
    voltage = ina.readBusVoltage_V();
    current = ina.readCurrent_A();
    power   = ina.readPower_W();
  }

  StaticJsonDocument<512> doc;
  doc["device_id"] = deviceId;
  doc["timestamp"] = nowUnix();
  doc["state"]     = curtailed ? "curtailed" : "generating";
  doc["seq"]       = seq;

  JsonObject m = doc.createNestedObject("metrics");
  m["voltage"]     = voltage;
  m["current"]     = current;
  m["power"]       = power;
  m["energy_wh"]   = 0;   // TODO: integrate over time if needed
  m["curtailed"]   = curtailed;

  publishJSON(topicTelemetry(), doc, true);
}

// ═══════════════════════════════════════════
// Status / Ack
// ═══════════════════════════════════════════

void publishStatus(const char *state) {
  StaticJsonDocument<256> doc;
  doc["device_id"] = deviceId;
  doc["timestamp"] = nowUnix();
  doc["state"]     = state;
  doc["seq"]       = seq;
  publishJSON(topicStatus(), doc, true);
}

void publishAck(const char *commandId, bool ok, const char *error) {
  StaticJsonDocument<256> doc;
  doc["command_id"] = commandId;
  doc["ok"]         = ok;
  if (!ok && strlen(error) > 0) {
    doc["error"] = error;
  }
  doc["timestamp"] = nowUnix();
  publishJSON(topicAck(), doc, false);
}

// ═══════════════════════════════════════════
// Command handler
// ═══════════════════════════════════════════

void onMessage(char *rawTopic, byte *payload, unsigned int length) {
  StaticJsonDocument<384> doc;
  DeserializationError err = deserializeJson(doc, payload, length);
  if (err) {
    publishAck("", false, "bad json");
    return;
  }

  const char *commandId = doc["command_id"] | "";
  const char *action    = doc["action"] | "";

  if (strcmp(action, "set_curtail") == 0) {
    bool on = doc["params"]["on"] | false;
    setCurtail(on);
    publishAck(commandId, true, "");
    publishTelemetry();
    return;
  }

  publishAck(commandId, false, "unsupported action");
}

// ═══════════════════════════════════════════
// Publish helper with optional HMAC signing
// ═══════════════════════════════════════════

void publishJSON(const String &topic, JsonDocument &doc, bool sign) {
  String payload;
  serializeJson(doc, payload);

#ifdef DEVICE_SECRET
  if (sign) {
    String signedPayload = signPayload(topic, payload, DEVICE_SECRET,
                                       nowUnix());
    if (signedPayload.length() > 0) {
      payload = signedPayload;
    }
  }
#endif

  bool ok = mqtt.publish(topic.c_str(), payload.c_str(), false);
  if (!ok) {
    Serial.printf("publish failed topic=%s\n", topic.c_str());
  }
}

// ═══════════════════════════════════════════
// Curtailment control
// ═══════════════════════════════════════════

void setCurtail(bool on) {
  curtailed = on;
  digitalWrite(curtailPin, on ? HIGH : LOW);
  digitalWrite(ledPin, on ? LOW : HIGH);
}

// ═══════════════════════════════════════════
// Unix timestamp
// ═══════════════════════════════════════════

long nowUnix() {
  time_t now = time(nullptr);
  if (now > 1700000000) return long(now);
  return long(millis() / 1000);
}
