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
- PostgreSQL 命令审计
- Redis 最新遥测状态缓存
- 设备模拟器
- ESP32 Arduino 固件模板
- Grafana 数据源自动配置

## 快速启动

```bash
cp .env.example .env
docker compose up -d
```

这会启动 EMQX、InfluxDB、PostgreSQL、Redis、Go 平台服务、simulator、Prometheus 和 Grafana。

Compose 已配置健康检查和启动顺序；查看状态：

```bash
docker compose ps
```

如果只想本机调试 Go 服务：

```bash
docker compose up -d emqx influxdb postgres redis grafana
go run ./cmd/vpp-lab
```

说明：`.env` 默认服务地址适合本机 `go run`；`docker-compose.yml` 会为容器化 `app` 覆盖为 `emqx/influxdb/postgres/redis` 这些 Compose 服务名。

Redis 是可选增强。设置 `REDIS_ADDR=localhost:6379` 后，平台会缓存每台设备的最新遥测，服务重启后可恢复实时 summary；不设置时仍使用内存状态。

本机调试时可另开终端启动模拟器：

```bash
go run ./cmd/simulator
```

查看状态：

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/metrics
curl http://localhost:8080/api/v1/devices
curl http://localhost:8080/api/v1/sites/home-lab/summary
curl http://localhost:8080/api/v1/sites/home-lab/device-states
curl http://localhost:8080/api/v1/sites/home-lab/plan
curl http://localhost:8080/api/v1/sites/home-lab/dispatch-preview
curl http://localhost:8080/api/v1/commands
```

`/healthz` 会检查 MQTT、PostgreSQL 和状态缓存；任一依赖异常时返回 HTTP 503。

`/api/v1/sites/{site_id}/device-states` 返回每台设备的元信息、最新遥测、在线状态和 stale 秒数，适合给自建控制台直接使用。

`/api/v1/commands` 返回最近 200 条命令记录。命令下发和设备回执会写入 PostgreSQL，服务重启后仍可查询。

`/metrics` 使用 Prometheus 文本格式输出站点功率、设备在线状态、设备最新遥测和命令状态计数。

生成自定义日前计划：

```bash
curl -X POST http://localhost:8080/api/v1/sites/home-lab/plan \
  -H 'Content-Type: application/json' \
  -d '{
    "horizon_hours": 24,
    "slot_minutes": 15,
    "battery_capacity_wh": 150,
    "battery_power_limit_w": 50,
    "min_soc": 0.25,
    "max_soc": 0.9,
    "tariffs": [
      {"name":"valley","start_hour":0,"end_hour":7,"price":0.32},
      {"name":"flat","start_hour":7,"end_hour":18,"price":0.58},
      {"name":"peak","start_hour":18,"end_hour":23,"price":0.95},
      {"name":"flat","start_hour":23,"end_hour":24,"price":0.58}
    ]
  }'
```

计划输出包含每个 15 分钟时隙的预测光伏、预测负载、电价、建议电池动作、目标功率和预测 SOC。当前它只生成建议，不直接控制设备；实时控制仍由规则调度器负责。

查看当前时隙的计划跟踪预览：

```bash
curl http://localhost:8080/api/v1/sites/home-lab/dispatch-preview
```

它会把当前实时状态和日前计划当前时隙对齐，输出：

- 当前时隙计划
- 实时净负荷和计划净负荷偏差
- 候选控制命令
- `safe_to_apply=false`

`safe_to_apply=false` 表示当前只是调度建议，不会自动下发到设备。

显式确认后按当前计划预览下发命令：

```bash
curl -X POST http://localhost:8080/api/v1/sites/home-lab/dispatch/apply \
  -H 'Content-Type: application/json' \
  -d '{
    "confirm": true,
    "max_abs_tracking_error_w": 100,
    "config": {
      "horizon_hours": 24,
      "slot_minutes": 15,
      "battery_capacity_wh": 150,
      "battery_power_limit_w": 50,
      "min_soc": 0.25,
      "max_soc": 0.9
    }
  }'
```

这个接口会重新计算当前时隙预览，只允许服务端生成的候选命令下发。没有 `confirm:true`、没有候选命令、或偏差超过 `max_abs_tracking_error_w` 时都会拒绝。

Grafana：

- URL: http://localhost:3000
- 用户名: `admin`
- 密码: `public`
- Overview Dashboard: http://localhost:3000/d/vpp-lab-overview/vpp-lab-overview
- Operations Dashboard: http://localhost:3000/d/vpp-lab-operations/vpp-lab-operations

Prometheus：

- URL: http://localhost:9090
- 抓取目标: `app:8080/metrics`

查看 Prometheus 告警规则状态：

```bash
curl http://localhost:9090/api/v1/rules
curl http://localhost:9090/api/v1/alerts
```

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
