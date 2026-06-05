package repository

import (
	"context"
	"encoding/json"
	"time"

	"vpp-lab/internal/model"
)

func (r *DeviceRepository) PutAuditLog(ctx context.Context, log model.AuditLog) error {
	if log.OccurredAt.IsZero() {
		log.OccurredAt = time.Now()
	}
	details, err := json.Marshal(log.Details)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
INSERT INTO audit_logs (
	id, occurred_at, actor, action, method, path, status_code, client_ip, user_agent, details
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb)
ON CONFLICT (id) DO NOTHING`,
		log.ID, log.OccurredAt, log.Actor, log.Action, log.Method, log.Path,
		log.StatusCode, log.ClientIP, log.UserAgent, string(details))
	return err
}

func (r *DeviceRepository) ListAuditLogs(ctx context.Context, limit int) ([]model.AuditLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := r.pool.Query(ctx, `
SELECT id, occurred_at, actor, action, method, path, status_code, client_ip, user_agent, details::text
FROM audit_logs
ORDER BY occurred_at DESC
LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.AuditLog
	for rows.Next() {
		var log model.AuditLog
		var detailsRaw string
		if err := rows.Scan(
			&log.ID,
			&log.OccurredAt,
			&log.Actor,
			&log.Action,
			&log.Method,
			&log.Path,
			&log.StatusCode,
			&log.ClientIP,
			&log.UserAgent,
			&detailsRaw,
		); err != nil {
			return nil, err
		}
		if detailsRaw != "" && detailsRaw != "null" {
			_ = json.Unmarshal([]byte(detailsRaw), &log.Details)
		}
		out = append(out, log)
	}
	return out, rows.Err()
}
