#pragma once
/*
  VPP Lab — ESP32 common utilities.

  Included by esp32-load / esp32-pv / esp32-battery sketches.

  Provides:
    - INA226 driver (pure I2C, no Adafruit dependency)
    - HMAC-SHA256 payload signing (mbedtls)
    - MQTT topic builders
    - JSON canonicalization for auth
*/

#include <ArduinoJson.h>
#include <Wire.h>
#include <mbedtls/md.h>

// ──────────────────────────────────────────────
// INA226 driver
// ──────────────────────────────────────────────

class INA226 {
public:
  bool begin(uint8_t addr = 0x40) {
    _addr = addr;
    Wire.beginTransmission(_addr);
    uint8_t err = Wire.endTransmission();
    if (err != 0) return false;

    // configure: continuous mode, 1.1 ms conv time, avg=1
    writeReg(0x00, 0x4527);
    // calibration for 1 mA / LSB current
    // Cal = 0.00512 / (Rshunt * current_LSB)
    // Rshunt = 0.01 ohm (10 mOhm), current_LSB = 1 mA
    // Cal = 0.00512 / (0.01 * 0.001) = 512
    writeReg(0x05, 512);
    return true;
  }

  float readBusVoltage_V() {
    // register 0x02, LSB = 1.25 mV
    uint16_t raw = readReg(0x02);
    return raw * 0.00125;
  }

  float readShuntVoltage_mV() {
    // register 0x01, LSB = 2.5 uV
    int16_t raw = (int16_t)readReg(0x01);
    return raw * 0.0025;
  }

  float readCurrent_A() {
    // register 0x04, LSB = 1 mA (with calibration above)
    int16_t raw = (int16_t)readReg(0x04);
    return raw * 0.001;
  }

  float readPower_W() {
    // register 0x03, LSB = 25 mW (with calibration above)
    uint16_t raw = readReg(0x03);
    return raw * 0.025;
  }

private:
  uint8_t _addr;

  void writeReg(uint8_t reg, uint16_t val) {
    Wire.beginTransmission(_addr);
    Wire.write(reg);
    Wire.write((uint8_t)(val >> 8));
    Wire.write((uint8_t)(val & 0xFF));
    Wire.endTransmission();
  }

  uint16_t readReg(uint8_t reg) {
    Wire.beginTransmission(_addr);
    Wire.write(reg);
    Wire.endTransmission(false);
    Wire.requestFrom(_addr, (uint8_t)2);
    uint16_t val = ((uint16_t)Wire.read() << 8) | Wire.read();
    return val;
  }
};

// ──────────────────────────────────────────────
// HMAC-SHA256 signing (mbedtls)
// ──────────────────────────────────────────────

static String hmacHex(const String &key, const uint8_t *data, size_t datalen) {
  uint8_t mac[32];
  mbedtls_md_context_t ctx;
  mbedtls_md_init(&ctx);
  mbedtls_md_setup(&ctx, mbedtls_md_info_from_type(MBEDTLS_MD_SHA256), 1);
  mbedtls_md_hmac_starts(&ctx, (const uint8_t *)key.c_str(), key.length());
  mbedtls_md_hmac_update(&ctx, data, datalen);
  mbedtls_md_hmac_finish(&ctx, mac);
  mbedtls_md_free(&ctx);

  char hex[65];
  for (int i = 0; i < 32; i++) {
    sprintf(hex + i * 2, "%02x", mac[i]);
  }
  hex[64] = '\0';
  return String(hex);
}

/*
  Sign a JSON payload per Go's deviceauth.SignPayload:
    1. parse payload into JSON object
    2. inject auth: {"timestamp": <unix>}
    3. marshal to canonical JSON with auth placeholder
    4. HMAC-SHA256(topic + "\n" + canonical, secret)
    5. inject full auth: {"timestamp": <unix>, "signature": <hex>}
    6. re-marshal and return

  Returns empty string on error.
*/
static String signPayload(const String &topic, const String &payload,
                          const String &secret, long timestamp) {
  // parse
  JsonDocument doc;
  DeserializationError err = deserializeJson(doc, payload);
  if (err) return String();

  // inject partial auth
  JsonObject authPlaceholder = doc["auth"].to<JsonObject>();
  authPlaceholder["timestamp"] = timestamp;

  // canonicalize: serialize without signature
  String canonical;
  serializeJson(doc, canonical);

  // compute HMAC
  String hmacInput = topic + "\n" + canonical;
  String sig = hmacHex(secret, (const uint8_t *)hmacInput.c_str(),
                       hmacInput.length());

  // inject full auth with signature
  authPlaceholder["signature"] = sig;

  // final output
  String result;
  serializeJson(doc, result);
  return result;
}

// ──────────────────────────────────────────────
// MQTT topic helpers
// ──────────────────────────────────────────────

static String mqttTopicTelemetry(const char *site, const char *type,
                                 const char *id) {
  return String("vpp/") + site + "/" + type + "/" + id + "/telemetry";
}

static String mqttTopicCommand(const char *site, const char *type,
                               const char *id) {
  return String("vpp/") + site + "/" + type + "/" + id + "/command";
}

static String mqttTopicAck(const char *site, const char *type,
                           const char *id) {
  return String("vpp/") + site + "/" + type + "/" + id + "/command/ack";
}

static String mqttTopicStatus(const char *site, const char *type,
                              const char *id) {
  return String("vpp/") + site + "/" + type + "/" + id + "/status";
}
