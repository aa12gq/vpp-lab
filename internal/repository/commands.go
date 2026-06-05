package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"vpp-lab/internal/model"
)

func (r *DeviceRepository) PutCommandIssued(ctx context.Context, siteID string, d model.Device, cmd model.Command) error {
	params, err := json.Marshal(cmd.Params)
	if err != nil {
		return err
	}
	issuedAt := time.Unix(cmd.IssuedAt, 0)
	_, err = r.pool.Exec(ctx, `
INSERT INTO command_records (
	command_id, site_id, device_id, device_type, action, params, reason, issued_at, status, updated_at
) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,'issued',now())
ON CONFLICT (command_id) DO UPDATE SET
	site_id = EXCLUDED.site_id,
	device_id = EXCLUDED.device_id,
	device_type = EXCLUDED.device_type,
	action = EXCLUDED.action,
	params = EXCLUDED.params,
	reason = EXCLUDED.reason,
	issued_at = EXCLUDED.issued_at,
	status = CASE
		WHEN command_records.ack_timestamp IS NULL THEN EXCLUDED.status
		ELSE command_records.status
	END,
	updated_at = now()`,
		cmd.CommandID, siteID, d.ID, string(d.Type), cmd.Action, string(params), cmd.Reason, issuedAt)
	return err
}

func (r *DeviceRepository) PutCommandAck(ctx context.Context, siteID string, deviceType model.DeviceType, deviceID string, ack model.CommandAck) error {
	status := "failed"
	if ack.OK {
		status = "acked"
	}
	ackAt := time.Unix(ack.Timestamp, 0)
	_, err := r.pool.Exec(ctx, `
INSERT INTO command_records (
	command_id, site_id, device_id, device_type, action, params, reason, issued_at,
	status, ack_ok, ack_error, ack_timestamp, updated_at
) VALUES ($1,$2,$3,$4,'','{}'::jsonb,'',$5,$6,$7,$8,$9,now())
ON CONFLICT (command_id) DO UPDATE SET
	status = EXCLUDED.status,
	ack_ok = EXCLUDED.ack_ok,
	ack_error = EXCLUDED.ack_error,
	ack_timestamp = EXCLUDED.ack_timestamp,
	updated_at = now()
WHERE command_records.device_id = EXCLUDED.device_id`,
		ack.CommandID, siteID, deviceID, string(deviceType), ackAt, status, ack.OK, ack.Error, ackAt)
	return err
}

func (r *DeviceRepository) ListCommands(ctx context.Context, limit int) ([]model.CommandRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := r.pool.Query(ctx, `
SELECT
	command_id, site_id, device_id, device_type, action, params::text, reason,
	EXTRACT(EPOCH FROM issued_at)::bigint,
	status,
	ack_ok,
	ack_error,
	EXTRACT(EPOCH FROM ack_timestamp)::bigint,
	updated_at
FROM command_records
ORDER BY updated_at DESC
LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.CommandRecord
	for rows.Next() {
		var rec model.CommandRecord
		var paramsRaw string
		var ackOK sql.NullBool
		var ackErr sql.NullString
		var ackTS sql.NullInt64
		var issuedAt int64
		if err := rows.Scan(
			&rec.Command.CommandID,
			&rec.SiteID,
			&rec.DeviceID,
			&rec.DeviceType,
			&rec.Command.Action,
			&paramsRaw,
			&rec.Command.Reason,
			&issuedAt,
			&rec.Status,
			&ackOK,
			&ackErr,
			&ackTS,
			&rec.UpdatedAt,
		); err != nil {
			return nil, err
		}
		rec.Command.IssuedAt = issuedAt
		if paramsRaw != "" && paramsRaw != "null" {
			_ = json.Unmarshal([]byte(paramsRaw), &rec.Command.Params)
		}
		if ackOK.Valid || ackErr.Valid || ackTS.Valid {
			rec.Ack = &model.CommandAck{
				CommandID: rec.Command.CommandID,
				OK:        ackOK.Bool,
				Error:     ackErr.String,
				Timestamp: ackTS.Int64,
			}
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}
