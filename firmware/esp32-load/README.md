# ESP32 Load Node Firmware

替代 simulator 中的 `load_01` / `load_02`，使用 INA226 实现真实负载功率监控 + 继电器/MOSFET 通断控制。

## 依赖

- ESP32 board package
- PubSubClient
- ArduinoJson

无需额外传感器库 — INA226 驱动已内置于 `esp32-common/vpp_common.h`。

## 硬件接线

| ESP32 | 模块 |
|-------|------|
| GPIO5 | 继电器/MOSFET 控制输入（高电平导通） |
| GPIO2 | 状态 LED（导通时常亮） |
| GPIO21 | I2C SDA → INA226 SDA |
| GPIO22 | I2C SCL → INA226 SCL |
| 3V3/GND | INA226 供电 |
| INA226 VBus+ | 负载输入电压 |
| INA226 VBus- / Shunt | 负载电流采样 |

## 必改配置

编辑 `esp32-load.ino` 顶部：

```cpp
const char *wifiSsid     = "YOUR_WIFI";
const char *wifiPassword = "YOUR_PASSWORD";
const char *mqttHost     = "192.168.1.10";
```

如需 HMAC 签名，取消注释并设置密钥：

```cpp
#define DEVICE_SECRET "your-device-secret"
```

## 编译

**PlatformIO（推荐）：**

```bash
cd firmware/esp32-load
pio run -t upload
```

**Arduino IDE：**

1. 将 `../esp32-common/vpp_common.h` 复制到 `esp32-load/` 目录
2. 将 `#include "../esp32-common/vpp_common.h"` 改为 `#include "vpp_common.h"`
3. 选择 ESP32 Dev Module，上传

## 通信协议

| Topic | 方向 | 说明 |
|-------|------|------|
| `vpp/{siteId}/load/{deviceId}/telemetry` | 上报 | 每 2s：电压、电流、功率、继电器状态 |
| `vpp/{siteId}/load/{deviceId}/command` | 订阅 | 接收 `set_relay` 指令 |
| `vpp/{siteId}/load/{deviceId}/command/ack` | 上报 | 命令执行回执 |
| `vpp/{siteId}/load/{deviceId}/status` | 上报 | 上线/离线状态 |

## 验证

```bash
curl -X POST http://localhost:8080/api/v1/devices/load_02/command \
  -H 'Content-Type: application/json' \
  -d '{"action":"set_relay","params":{"on":true},"reason":"test"}'
```

## 与旧版 esp32-node 的区别

- 移除 `USE_INA219` 条件编译，直接使用内置 INA226 驱动
- 加入可选的 HMAC-SHA256 签名（`DEVICE_SECRET`）
- 增大 MQTT buffer 至 1024
- 改进 WiFi 重试机制（超时退出，不卡死）
