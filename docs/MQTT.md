# MQTT 协议约定

## Topic

```text
vpp/{site_id}/{device_type}/{device_id}/telemetry
vpp/{site_id}/{device_type}/{device_id}/status
vpp/{site_id}/{device_type}/{device_id}/command
vpp/{site_id}/{device_type}/{device_id}/command/ack
vpp/{site_id}/{device_type}/{device_id}/event
```

`device_type` 当前支持：

- `pv`
- `battery`
- `load`

## 遥测上报

```json
{
  "device_id": "battery_01",
  "timestamp": 1733368800,
  "metrics": {
    "voltage": 12.45,
    "current": 2.31,
    "power": 28.76,
    "soc": 0.78,
    "temperature": 25.3
  },
  "state": "charging",
  "seq": 10234
}
```

说明：

- `seq` 用于去重和丢包检测。
- `power` 对 PV/负载使用正值。
- 电池 `power` 暂按放电为正、充电为负预留；当前规则调度主要使用 SOC。

## 控制命令

```json
{
  "command_id": "load_02-1733368800000",
  "action": "set_relay",
  "params": {
    "on": false
  },
  "issued_at": 1733368800,
  "reason": "load shed"
}
```

当前动作：

- `set_relay`: 控制负载开关
- `set_mode`: 控制电池模式，`charge` / `discharge` / `idle`

## 命令回执

```json
{
  "command_id": "load_02-1733368800000",
  "ok": true,
  "timestamp": 1733368801
}
```
