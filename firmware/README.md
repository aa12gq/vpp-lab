# VPP Lab — ESP32 固件

将 simulator 中的虚拟设备替换为真实物理硬件，统一使用 INA226 作功率监测，支持 HMAC-SHA256 消息签名。

## 目录结构

```
firmware/
├── esp32-common/          ← 公共代码（单头文件）
│   └── vpp_common.h       ← INA226 驱动 + HMAC 签名 + Topic 工具
├── esp32-load/            ← 负载节点（替代 load_01/load_02）
│   ├── esp32-load.ino
│   └── README.md
├── esp32-pv/              ← 光伏节点（替代 pv_01/pv_02）
│   ├── esp32-pv.ino
│   └── README.md
├── esp32-battery/         ← 储能节点（替代 battery_01）
│   ├── esp32-battery.ino
│   └── README.md
├── esp32-node/            ← 旧版单节点（仅作参考，不推荐使用）
│   ├── esp32-node.ino
│   └── README.md
└── README.md              ← 本文件
```

## 三种节点对比

| 特性 | Load | PV | Battery |
|------|------|----|---------|
| 传感器 | INA226 | INA226 | INA226（双向） |
| 控制 | 继电器 GPIO5 | 限发 MOSFET GPIO5 | 充电 MOSFET GPIO5 + 放电 MOSFET GPIO4 |
| 输入指令 | `set_relay` | `set_curtail` | `set_mode` |
| 状态 | on/off | generating/curtailed | idle/charging/discharging |
| HMAC 签名 | ✅ 可选 | ✅ 可选 | ✅ 可选 |

## 快速开始

### 1. 配置网络和平台地址

在每个 `.ino` 文件顶部修改：

```cpp
const char *wifiSsid     = "YOUR_WIFI";
const char *wifiPassword = "YOUR_PASSWORD";
const char *mqttHost     = "192.168.1.10";   // 平台 EMQX 地址
```

### 2. （可选）启用设备签名

取消注释 `#define DEVICE_SECRET` 并填入与平台 `device_keys` 表一致的密钥。

### 3. 编译烧录

**PlatformIO（推荐）：**

```bash
cd firmware/esp32-load   # 或 esp32-pv, esp32-battery
pio run -t upload
```

**Arduino IDE：**

将 `esp32-common/vpp_common.h` 复制到各 sketch 目录后，把 include 路径改为相对路径。

### 4. 验证

烧录后查看串口输出：

```
wifi connected ip=192.168.x.x
clock unix=17...
mqtt connected cmd_topic=vpp/{siteId}/{type}/{deviceId}/command
```

平台端查看设备上线：

```bash
curl http://localhost:8080/api/v1/sites/home-lab/device-states
```

## 迁移指南（simulator → 真实硬件）

1. 先在平台关闭对应 simulator 设备（或修改 MQTT clientId 避免冲突）
2. 烧录固件后确认 telemetry 正常上报
3. 逐个下发控制指令验证闭环
4. 确认无误后在平台删除 simulator 设备
