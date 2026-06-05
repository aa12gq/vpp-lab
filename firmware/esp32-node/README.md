# ESP32 Load Node Firmware

这是 `load_02` 真实低压负载节点固件模板，用于替代 simulator 中的一个负载设备。

## 依赖

Arduino IDE / Arduino CLI 安装：

- ESP32 board package
- `PubSubClient`
- `ArduinoJson`
- `Adafruit INA219`，仅在 `USE_INA219=1` 时需要

## 默认硬件

| ESP32 | 模块 |
|---|---|
| GPIO5 | 继电器/MOSFET 控制输入 |
| GPIO2 | 状态 LED |
| GPIO21 | I2C SDA |
| GPIO22 | I2C SCL |
| 3V3/GND | INA219/INA226 供电 |

第一版建议先用 `USE_INA219=0`，固件会用固定 12V 和模拟电流上报，先验证 MQTT 和控制闭环。接线稳定后再打开真实功率采集。

## 必改配置

在 `esp32-node.ino` 顶部修改：

```cpp
const char *wifiSsid = "YOUR_WIFI";
const char *wifiPassword = "YOUR_PASSWORD";
const char *mqttHost = "192.168.1.10";
const int mqttPort = 1883;

const char *siteId = "home-lab";
const char *deviceType = "load";
const char *deviceId = "load_02";
```

如果 EMQX 开启了用户名密码：

```cpp
const char *mqttUsername = "your-user";
const char *mqttPassword = "your-password";
```

## 平台配置

首次接入真实 ESP32 时，建议先不要启用平台 `DEVICE_KEYS`。当前固件默认不生成 HMAC `auth` 字段；如果平台配置了 `DEVICE_KEYS`，会拒收该节点消息。

## 验证

1. 启动本地平台：

```bash
docker compose up -d
make smoke
```

2. 烧录 ESP32 后观察串口：

```text
wifi connected ip=...
clock unix=...
mqtt connected command_topic=vpp/home-lab/load/load_02/command
```

3. 查看设备状态：

```bash
curl http://localhost:8080/api/v1/sites/home-lab/device-states
```

4. 下发继电器控制：

```bash
curl -X POST http://localhost:8080/api/v1/devices/load_02/command \
  -H 'Content-Type: application/json' \
  -d '{"action":"set_relay","params":{"on":true},"reason":"esp32 test"}'
```

5. 查看命令回执：

```bash
curl http://localhost:8080/api/v1/commands
```

## 安全

- 只接 5V/12V/24V 低压直流。
- 不接 220V 市电。
- 继电器/MOSFET 先控制 LED、小风扇等低压负载。
- 用万用表确认接线后再上电。
