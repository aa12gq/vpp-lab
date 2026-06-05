package edge

import (
	"context"
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

type CachedMessage struct {
	ID        int64     `json:"id"`
	Topic     string    `json:"topic"`
	Payload   []byte    `json:"payload"`
	CreatedAt time.Time `json:"created_at"`
	SentAt    time.Time `json:"sent_at,omitempty"`
}

type Cache struct {
	db *sql.DB
}

func OpenCache(ctx context.Context, path string) (*Cache, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	cache := &Cache{db: db}
	if err := cache.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return cache, nil
}

func (c *Cache) Close() error {
	return c.db.Close()
}

func (c *Cache) migrate(ctx context.Context) error {
	_, err := c.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS mqtt_messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	topic TEXT NOT NULL,
	payload BLOB NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	sent_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_mqtt_messages_sent_at ON mqtt_messages (sent_at, id);
`)
	return err
}

func (c *Cache) Put(ctx context.Context, topic string, payload []byte) error {
	_, err := c.db.ExecContext(ctx, `
INSERT INTO mqtt_messages (topic, payload, created_at)
VALUES (?, ?, ?)`, topic, payload, time.Now().UTC())
	return err
}

func (c *Cache) Pending(ctx context.Context, limit int) ([]CachedMessage, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := c.db.QueryContext(ctx, `
SELECT id, topic, payload, created_at
FROM mqtt_messages
WHERE sent_at IS NULL
ORDER BY id ASC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CachedMessage
	for rows.Next() {
		var msg CachedMessage
		if err := rows.Scan(&msg.ID, &msg.Topic, &msg.Payload, &msg.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

func (c *Cache) MarkSent(ctx context.Context, id int64) error {
	_, err := c.db.ExecContext(ctx, `UPDATE mqtt_messages SET sent_at = ? WHERE id = ?`, time.Now().UTC(), id)
	return err
}

func (c *Cache) Stats(ctx context.Context) (pending int64, total int64, err error) {
	if err = c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mqtt_messages WHERE sent_at IS NULL`).Scan(&pending); err != nil {
		return 0, 0, err
	}
	if err = c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mqtt_messages`).Scan(&total); err != nil {
		return 0, 0, err
	}
	return pending, total, nil
}
