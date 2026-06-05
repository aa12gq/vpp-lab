package repository

import (
	"context"
	"encoding/json"
	"time"

	"vpp-lab/internal/model"
)

func (r *DeviceRepository) PutEvent(ctx context.Context, event model.DeviceEvent) error {
	details, err := json.Marshal(event.Details)
	if err != nil {
		return err
	}
	eventAt := time.Unix(event.Timestamp, 0)
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	_, err = r.pool.Exec(ctx, `
INSERT INTO device_events (
	event_id, site_id, device_id, device_type, severity, code, message, details, event_timestamp, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10)
ON CONFLICT (event_id) DO UPDATE SET
	site_id = EXCLUDED.site_id,
	device_id = EXCLUDED.device_id,
	device_type = EXCLUDED.device_type,
	severity = EXCLUDED.severity,
	code = EXCLUDED.code,
	message = EXCLUDED.message,
	details = EXCLUDED.details,
	event_timestamp = EXCLUDED.event_timestamp,
	created_at = EXCLUDED.created_at`,
		event.EventID, event.SiteID, event.DeviceID, string(event.DeviceType), event.Severity, event.Code,
		event.Message, string(details), eventAt, event.CreatedAt)
	return err
}

func (r *DeviceRepository) ListEvents(ctx context.Context, limit int) ([]model.DeviceEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := r.pool.Query(ctx, `
SELECT
	event_id, site_id, device_id, device_type, severity, code, message, details::text,
	EXTRACT(EPOCH FROM event_timestamp)::bigint,
	created_at
FROM device_events
ORDER BY created_at DESC
LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.DeviceEvent
	for rows.Next() {
		var event model.DeviceEvent
		var detailsRaw string
		if err := rows.Scan(
			&event.EventID,
			&event.SiteID,
			&event.DeviceID,
			&event.DeviceType,
			&event.Severity,
			&event.Code,
			&event.Message,
			&detailsRaw,
			&event.Timestamp,
			&event.CreatedAt,
		); err != nil {
			return nil, err
		}
		if detailsRaw != "" && detailsRaw != "null" {
			_ = json.Unmarshal([]byte(detailsRaw), &event.Details)
		}
		out = append(out, event)
	}
	return out, rows.Err()
}
