package model

import "time"

type DeviceType string

const (
	DevicePV      DeviceType = "pv"
	DeviceBattery DeviceType = "battery"
	DeviceLoad    DeviceType = "load"
)

type Device struct {
	ID           string     `json:"id"`
	SiteID       string     `json:"site_id"`
	Type         DeviceType `json:"type"`
	Name         string     `json:"name"`
	RatedPowerW  float64    `json:"rated_power_w"`
	CapacityWh   float64    `json:"capacity_wh,omitempty"`
	CriticalLoad bool       `json:"critical_load,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

type Telemetry struct {
	DeviceID  string             `json:"device_id"`
	SiteID    string             `json:"site_id,omitempty"`
	Type      DeviceType         `json:"device_type,omitempty"`
	Timestamp int64              `json:"timestamp"`
	Metrics   map[string]float64 `json:"metrics"`
	State     string             `json:"state,omitempty"`
	Seq       int64              `json:"seq"`
}

type Command struct {
	CommandID string                 `json:"command_id"`
	Action    string                 `json:"action"`
	Params    map[string]interface{} `json:"params,omitempty"`
	IssuedAt  int64                  `json:"issued_at"`
	Reason    string                 `json:"reason,omitempty"`
}

type CommandAck struct {
	CommandID string `json:"command_id"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

type SiteSummary struct {
	SiteID       string    `json:"site_id"`
	PVPowerW     float64   `json:"pv_power_w"`
	LoadPowerW   float64   `json:"load_power_w"`
	BatteryPower float64   `json:"battery_power_w"`
	NetPowerW    float64   `json:"net_power_w"`
	AvgSOC       float64   `json:"avg_soc"`
	LastUpdated  time.Time `json:"last_updated"`
}

type Policy struct {
	BatteryMinSOC     float64 `json:"battery_min_soc"`
	BatteryMaxSOC     float64 `json:"battery_max_soc"`
	LoadShedThreshold float64 `json:"load_shed_threshold_w"`
}
