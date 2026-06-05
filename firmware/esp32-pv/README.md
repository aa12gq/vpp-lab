# ESP32 PV Node Firmware

替代 simulator 中的 `pv_01` / `pv_02`，使用 INA226 监测光伏板发电功率，支持远程限发（curtailment）控制。

## 依赖

- ESP32 board package
- PubSubClient
- ArduinoJson

INA226 驱动内置于 `esp32-common/vpp_common.h`。

## 硬件接线

| ESP32 | 模块 |
|-------|------|
| GPIO5 | 限发 MOSFET 控制（HIGH = 切断 PV） |
| GPIO2 | 状态 LED（生成时常亮，限发时熄灭） |
| GPIO21 | I2C SDA → INA226 SDA |
| GPIO22 | I2C SCL → INA226 SCL |
| 3V3/GND | INA226 供电 |
| INA226 VBus+ | PV 输出电压 |
| INA226 VBus- / Shunt | PV 输出电流 |

## 必改配置

```cpp
const char *wifiSsid     = "YOUR_WIFI";
const char *wifiPassword = "YOUR_PASSWORD";
const char *mqttHost     = "192.168.1.10";
```

可选 HMAC 签名：

```cpp
#define DEVICE_SECRET "your-device-secret"
```

## 通信协议

| Topic | 方向 | 说明 |
|-------|------|------|
| `vpp/{siteId}/pv/{deviceId}/telemetry` | 上报 | 每 2s：电压、电流、功率、限发状态 |
| `vpp/{siteId}/pv/{deviceId}/command` | 订阅 | 接收 `set_curtail` 指令 |
| `vpp/{siteId}/pv/{deviceId}/command/ack` | 上报 | 命令执行回执 |
| `vpp/{siteId}/pv/{deviceId}/status` | 上报 | 上线/离线状态 |

### 限发指令示例

```bash
# 启用限发（切断 PV）
curl -X POST http://localhost:8080/api/v1/devices/pv_01/command \
  -H 'Content-Type: application/json' \
  -d '{"action":"set_curtail","params":{"on":true},"reason":"grid overload"}'

# 恢复发电
curl -X POST http://localhost:8080/api/v1/devices/pv_01/command \
  -H 'Content-Type: application/json' \
  -d '{"action":"set_curtail","params":{"on":false},"reason":"grid stable"}'
```

## 安全

- PV 板开路电压不得超过 INA226 最大输入（36V）
- 限发 MOSFET 建议使用 N-channel 逻辑电平管，配合负载电阻放电
- 大功率场景下注意 shunt 电阻的散热
