# ESP32 Battery Node Firmware

替代 simulator 中的 `battery_01`，使用 INA226 实现双向电流检测 + 独立充放电 MOSFET 控制。

## 状态机

```
        ┌─────────┐
        │  idle   │ ←─ 初始状态，两个 MOSFET 均关闭
        └────┬────┘
      ┌──────┴──────┐
      ▼              ▼
 ┌─────────┐  ┌────────────┐
 │charging │  │discharging │
 │(充电路径)│  │(放电路径)   │
 └─────────┘  └────────────┘
```

## 依赖

- ESP32 board package
- PubSubClient
- ArduinoJson

INA226 驱动内置于 `esp32-common/vpp_common.h`。

## 硬件接线

| ESP32 | 模块 |
|-------|------|
| GPIO5 | 充电 MOSFET（HIGH = 允许充电） |
| GPIO4 | 放电 MOSFET（HIGH = 允许放电） |
| GPIO2 | 状态 LED（idle 时熄灭，充/放时常亮） |
| GPIO21 | I2C SDA → INA226 SDA |
| GPIO22 | I2C SCL → INA226 SCL |
| INA226 VBus+ | 电池端电压 |
| INA226 Shunt | 双向电流采样（正=充电，负=放电） |

## 必改配置

```cpp
const char *wifiSsid     = "YOUR_WIFI";
const char *wifiPassword = "YOUR_PASSWORD";
const char *mqttHost     = "192.168.1.10";
```

### 电池参数

根据实际电池化学体系修改：

```cpp
const float batteryFullV    = 12.6;   // 3S Li-ion 满电
const float batteryEmptyV   = 9.0;    // 3S Li-ion 截止
const float ratedCapacityAh = 10.0;   // 标称容量 Ah
```

可选 HMAC 签名：

```cpp
#define DEVICE_SECRET "your-device-secret"
```

## 通信协议

| Topic | 方向 | 说明 |
|-------|------|------|
| `vpp/{siteId}/battery/{deviceId}/telemetry` | 上报 | 每 2s：电压、电流、功率、SOC、当前模式 |
| `vpp/{siteId}/battery/{deviceId}/command` | 订阅 | 接收 `set_mode` 指令 |
| `vpp/{siteId}/battery/{deviceId}/command/ack` | 上报 | 命令执行回执 |
| `vpp/{siteId}/battery/{deviceId}/status` | 上报 | 上线/离线状态 |

### 模式切换指令

```bash
# 切换为充电模式
curl -X POST http://localhost:8080/api/v1/devices/battery_01/command \
  -H 'Content-Type: application/json' \
  -d '{"action":"set_mode","params":{"mode":"charging"},"reason":"excess solar"}'

# 切换为放电模式
curl -X POST http://localhost:8080/api/v1/devices/battery_01/command \
  -H 'Content-Type: application/json' \
  -d '{"action":"set_mode","params":{"mode":"discharging"},"reason":"peak shaving"}'

# 切换为空闲
curl -X POST http://localhost:8080/api/v1/devices/battery_01/command \
  -H 'Content-Type: application/json' \
  -d '{"action":"set_mode","params":{"mode":"idle"},"reason":"maintenance"}'
```

## SOC 估算

当前使用电压线性插值法（3S Li-ion），精度约 ±10%。如需更精确的 SOC：

1. 替换 `estimateSOC()` 为自定义查表函数
2. 或外接 BMS（如 BQ769x 系列）通过 UART/I2C 读取真实 SOC

## 安全注意事项

- 两个 MOSFET 不可同时导通（固件已通过 `setMode()` 互斥逻辑保证）
- 充放电路径应分别加装保险丝
- INA226 最大共模电压 36V，适用于 12V/24V 电池系统
- 建议在充放 MOSFET 上加装 RC 吸收电路防止开关尖峰
