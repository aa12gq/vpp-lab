# VPP Lab

小型虚拟电厂实验平台。目标是在家用低压硬件跑通：

```text
采集 -> MQTT -> Go 平台 -> InfluxDB/PostgreSQL -> Grafana/API -> 调度 -> 控制
```

## 当前能力

- EMQX MQTT Broker
- InfluxDB 时序数据写入
- PostgreSQL 设备元信息
- Go HTTP API
- Go 实时规则调度
- 设备模拟器
- ESP32 Arduino 固件模板
- Grafana 数据源自动配置

## 快速启动

```bash
cp .env.example .env
docker compose up -d emqx influxdb postgres redis grafana
go run ./cmd/vpp-lab
```

另开终端启动模拟器：

```bash
go run ./cmd/simulator
```

查看状态：

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/api/v1/devices
curl http://localhost:8080/api/v1/sites/home-lab/summary
curl http://localhost:8080/api/v1/sites/home-lab/plan
curl http://localhost:8080/api/v1/commands
```

Grafana：

- URL: http://localhost:3000
- 用户名: `admin`
- 密码: `public`

EMQX Dashboard：

- URL: http://localhost:18083
- 用户名: `admin`
- 密码: `public`

## 手动下发命令

```bash
curl -X POST http://localhost:8080/api/v1/devices/load_02/command \
  -H 'Content-Type: application/json' \
  -d '{"action":"set_relay","params":{"on":false},"reason":"manual test"}'
```

如果 simulator 正在运行，它会订阅命令、修改本地状态并发布 `command/ack`。验证：

```bash
curl http://localhost:8080/api/v1/commands
curl http://localhost:8080/api/v1/sites/home-lab/summary
```

重新打开负载：

```bash
curl -X POST http://localhost:8080/api/v1/devices/load_02/command \
  -H 'Content-Type: application/json' \
  -d '{"action":"set_relay","params":{"on":true},"reason":"manual restore"}'
```

## 注册设备

```bash
curl -X POST http://localhost:8080/api/v1/devices \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "load_03",
    "site_id": "home-lab",
    "type": "load",
    "name": "Desk Fan",
    "rated_power_w": 20,
    "critical_load": false
  }'
```

## 调度策略

查看：

```bash
curl http://localhost:8080/api/v1/policies/default
```

修改：

```bash
curl -X PUT http://localhost:8080/api/v1/policies/default \
  -H 'Content-Type: application/json' \
  -d '{"battery_min_soc":0.25,"battery_max_soc":0.9,"load_shed_threshold_w":80}'
```

## 目录

```text
cmd/vpp-lab             平台服务
cmd/simulator           无硬件模拟器
firmware/esp32-node     ESP32 固件模板
internal/api            HTTP API
internal/mqtt           MQTT 接入
internal/scheduler      实时规则调度
internal/optimizer      简单 24h 计划
docs                    架构、硬件、协议文档
```

## 下一步

1. 先用 simulator 跑通全链路。
2. 再烧录 `firmware/esp32-node`，替换一个真实负载节点。
3. 接入 INA226 后，把固件里的模拟电压/电流替换为真实读数。
4. 增加 PV 节点和 battery 节点。
