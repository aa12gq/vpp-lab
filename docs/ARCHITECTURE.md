# 小型虚拟电厂架构方案

## 定位

本项目定位为个人/小团队可落地的小型 VPP 实验平台，覆盖：

- 阶段 1：单设备闭环
- 阶段 2：多设备聚合
- 阶段 3：规则调度
- 阶段 4：简单预测/优化计划

阶段 5 的工程化能力通过模块边界预留，不在首版堆复杂度。

## 分层

```text
ESP32 感知层
  -> MQTT 网络层
  -> Go 平台层
  -> InfluxDB/PostgreSQL 数据层
  -> Grafana/HTTP API 应用层
```

## 当前实现

首版采用模块化单体：

```text
cmd/vpp-lab
internal/api          HTTP API
internal/mqtt         MQTT 接入和命令发布
internal/scheduler    实时规则调度
internal/optimizer    24h 简单计划生成
internal/repository   PostgreSQL 设备数据
internal/timeseries   InfluxDB 遥测写入
internal/state        内存实时状态
```

这样做的原因：

- 家用实验项目先保证能跑通闭环。
- Go 模块边界已经按服务职责切好。
- 后续拆成设备服务、数据服务、调度服务时，接口基本不变。

## 数据流

```text
ESP32/simulator
  -> EMQX
  -> Go MQTT subscriber
  -> memory state
  -> InfluxDB
  -> scheduler
  -> MQTT command
  -> ESP32 command/ack
```

设备元信息：

```text
HTTP API / seed data -> PostgreSQL -> memory state
```

命令审计：

```text
API / scheduler / dispatch apply
  -> MQTT command
  -> memory command state
  -> PostgreSQL command_records
  -> MQTT command/ack
  -> memory + PostgreSQL ack update
```

## 调度策略

实时调度周期默认 5 秒。

规则：

- 光伏过剩且电池 SOC 未满：电池充电
- 负荷缺口且电池 SOC 足够：电池放电
- 负荷缺口超过阈值：关闭非关键负载

日前计划：

- 基于当前站点状态生成未来 24 小时计划
- 默认粒度 15 分钟
- 输入包括分时电价、电池容量、功率上限、SOC 上下限
- 输出预测光伏、预测负载、建议充放电动作、目标功率、预测 SOC
- 当前只作为建议计划，不直接驱动设备控制

计划跟踪预览：

- 将当前实时状态和日前计划当前时隙对齐
- 计算实时净负荷与计划净负荷偏差
- 生成候选控制命令
- 默认 `safe_to_apply=false`，避免预览逻辑直接误控设备

受控下发：

- `dispatch/apply` 会重新计算当前预览，不接受客户端传入的命令体
- 请求必须显式传入 `confirm:true`
- 可通过 `max_abs_tracking_error_w` 限制实时偏差
- 通过校验后才发布 MQTT 控制命令

监控指标：

- `/metrics` 输出 Prometheus 文本格式
- 包含站点功率、平均 SOC、设备在线状态、设备遥测和命令状态计数
- 设备在线状态基于最近遥测时间计算，默认 30 秒内视为在线
- Docker Compose 内置 Prometheus，默认抓取 `app:8080/metrics`
- Prometheus 内置基础规则：设备离线、命令失败、净负荷过高
- Grafana 自动配置 InfluxDB 和 Prometheus 两个数据源
- Grafana 内置 Overview 和 Operations 两个 dashboard
- Docker Compose 内置 simulator 服务，用于一键演示完整数据闭环
- Docker Compose 使用 healthcheck 和 `service_healthy` 降低启动竞态
- `internal/state` 支持可选 Redis 后端，缓存每台设备最新遥测，服务重启后恢复实时 summary

## 阶段 5 扩展点

- Redis 状态缓存扩展为多实例共享状态、过期策略和状态变更事件
- `internal/mqtt` 增加 TLS、用户名密码、设备证书
- `internal/scheduler` 拆成独立服务
- `internal/optimizer` 替换为 Python gRPC 优化服务
- 增加边缘网关模块：SQLite 缓存、离线自治、Modbus 适配器
