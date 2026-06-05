package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"vpp-lab/internal/model"
)

type DeviceRepository struct {
	pool *pgxpool.Pool
}

func NewDeviceRepository(ctx context.Context, dsn string) (*DeviceRepository, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	r := &DeviceRepository{pool: pool}
	if err := r.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return r, nil
}

func (r *DeviceRepository) Close() {
	r.pool.Close()
}

func (r *DeviceRepository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func (r *DeviceRepository) migrate(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS devices (
	id TEXT PRIMARY KEY,
	site_id TEXT NOT NULL,
	type TEXT NOT NULL,
	name TEXT NOT NULL,
	rated_power_w DOUBLE PRECISION NOT NULL DEFAULT 0,
	capacity_wh DOUBLE PRECISION NOT NULL DEFAULT 0,
	critical_load BOOLEAN NOT NULL DEFAULT FALSE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS command_records (
	command_id TEXT PRIMARY KEY,
	site_id TEXT NOT NULL,
	device_id TEXT NOT NULL,
	device_type TEXT NOT NULL,
	action TEXT NOT NULL,
	params JSONB NOT NULL DEFAULT '{}'::jsonb,
	reason TEXT NOT NULL DEFAULT '',
	issued_at TIMESTAMPTZ NOT NULL,
	status TEXT NOT NULL DEFAULT 'issued',
	ack_ok BOOLEAN,
	ack_error TEXT,
	ack_timestamp TIMESTAMPTZ,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_command_records_updated_at ON command_records (updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_command_records_device_id ON command_records (device_id);
CREATE TABLE IF NOT EXISTS device_events (
	event_id TEXT PRIMARY KEY,
	site_id TEXT NOT NULL,
	device_id TEXT NOT NULL,
	device_type TEXT NOT NULL,
	severity TEXT NOT NULL,
	code TEXT NOT NULL,
	message TEXT NOT NULL,
	details JSONB NOT NULL DEFAULT '{}'::jsonb,
	event_timestamp TIMESTAMPTZ NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_device_events_created_at ON device_events (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_device_events_device_id ON device_events (device_id);
CREATE INDEX IF NOT EXISTS idx_device_events_severity ON device_events (severity);
CREATE TABLE IF NOT EXISTS audit_logs (
	id TEXT PRIMARY KEY,
	occurred_at TIMESTAMPTZ NOT NULL,
	actor TEXT NOT NULL,
	action TEXT NOT NULL,
	method TEXT NOT NULL,
	path TEXT NOT NULL,
	status_code INTEGER NOT NULL,
	client_ip TEXT NOT NULL,
	user_agent TEXT NOT NULL DEFAULT '',
	details JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_occurred_at ON audit_logs (occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs (action);
`)
	return err
}

func (r *DeviceRepository) Upsert(ctx context.Context, d model.Device) error {
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now()
	}
	_, err := r.pool.Exec(ctx, `
INSERT INTO devices (id, site_id, type, name, rated_power_w, capacity_wh, critical_load, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (id) DO UPDATE SET
	site_id = EXCLUDED.site_id,
	type = EXCLUDED.type,
	name = EXCLUDED.name,
	rated_power_w = EXCLUDED.rated_power_w,
	capacity_wh = EXCLUDED.capacity_wh,
	critical_load = EXCLUDED.critical_load`,
		d.ID, d.SiteID, string(d.Type), d.Name, d.RatedPowerW, d.CapacityWh, d.CriticalLoad, d.CreatedAt)
	return err
}

func (r *DeviceRepository) List(ctx context.Context) ([]model.Device, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, site_id, type, name, rated_power_w, capacity_wh, critical_load, created_at
FROM devices ORDER BY site_id, type, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Device
	for rows.Next() {
		var d model.Device
		if err := rows.Scan(&d.ID, &d.SiteID, &d.Type, &d.Name, &d.RatedPowerW, &d.CapacityWh, &d.CriticalLoad, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
