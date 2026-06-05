#include <ArduinoJson.h>
#include <PubSubClient.h>
#include <WiFi.h>

const char *wifiSsid = "YOUR_WIFI";
const char *wifiPassword = "YOUR_PASSWORD";
const char *mqttHost = "192.168.1.10";
const int mqttPort = 1883;

const char *siteId = "home-lab";
const char *deviceType = "load";
const char *deviceId = "load_02";

const int relayPin = 5;
const int ledPin = 2;

WiFiClient wifiClient;
PubSubClient mqtt(wifiClient);
long seq = 0;
bool relayOn = false;

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
  digitalWrite(relayPin, LOW);
  Serial.begin(115200);

  WiFi.begin(wifiSsid, wifiPassword);
  while (WiFi.status() != WL_CONNECTED) {
    delay(500);
    Serial.print(".");
  }
  Serial.println("wifi connected");

  mqtt.setServer(mqttHost, mqttPort);
  mqtt.setCallback(onMessage);
}

void loop() {
  if (!mqtt.connected()) {
    reconnectMQTT();
  }
  mqtt.loop();

  static unsigned long lastPublish = 0;
  if (millis() - lastPublish > 2000) {
    lastPublish = millis();
    publishTelemetry();
  }
}

void reconnectMQTT() {
  while (!mqtt.connected()) {
    String clientId = String("esp32-") + deviceId;
    if (mqtt.connect(clientId.c_str())) {
      mqtt.subscribe(topicCommand().c_str());
    } else {
      delay(1000);
    }
  }
}

void publishTelemetry() {
  seq++;
  StaticJsonDocument<256> doc;
  doc["device_id"] = deviceId;
  doc["timestamp"] = time(nullptr);
  doc["state"] = relayOn ? "on" : "off";
  doc["seq"] = seq;

  JsonObject metrics = doc.createNestedObject("metrics");
  metrics["voltage"] = 12.0;
  metrics["current"] = relayOn ? 1.2 : 0.0;
  metrics["power"] = metrics["voltage"].as<float>() * metrics["current"].as<float>();

  char payload[256];
  serializeJson(doc, payload);
  mqtt.publish(topicTelemetry().c_str(), payload);
}

void onMessage(char *topic, byte *payload, unsigned int length) {
  StaticJsonDocument<256> doc;
  DeserializationError err = deserializeJson(doc, payload, length);
  if (err) {
    return;
  }

  const char *commandId = doc["command_id"] | "";
  const char *action = doc["action"] | "";

  bool ok = true;
  const char *error = "";
  if (strcmp(action, "set_relay") == 0) {
    relayOn = doc["params"]["on"] | false;
    digitalWrite(relayPin, relayOn ? HIGH : LOW);
    digitalWrite(ledPin, relayOn ? HIGH : LOW);
  } else {
    ok = false;
    error = "unsupported action";
  }

  StaticJsonDocument<192> ack;
  ack["command_id"] = commandId;
  ack["ok"] = ok;
  ack["error"] = error;
  ack["timestamp"] = time(nullptr);
  char ackPayload[192];
  serializeJson(ack, ackPayload);
  mqtt.publish(topicAck().c_str(), ackPayload);
}
