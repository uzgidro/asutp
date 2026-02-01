package buffer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/speedwagon-io/asutp/internal/lib/logger/sl"
	"github.com/speedwagon-io/asutp/internal/model"
)

type Buffer interface {
	Store(ctx context.Context, envelope *model.Envelope) error
	GetPending(ctx context.Context, limit int) ([]*model.Envelope, error)
	MarkSent(ctx context.Context, ids []string) error
	Cleanup(ctx context.Context, maxAge time.Duration) error
	Close() error
}

type SQLiteBuffer struct {
	log *slog.Logger
	db  *sql.DB
}

func NewSQLiteBuffer(log *slog.Logger, dbPath string) (*SQLiteBuffer, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create buffer directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	buf := &SQLiteBuffer{
		log: log,
		db:  db,
	}

	if err := buf.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return buf, nil
}

func (b *SQLiteBuffer) migrate() error {
	query := `
		CREATE TABLE IF NOT EXISTS buffer (
			id TEXT PRIMARY KEY,
			station_id TEXT NOT NULL,
			station_name TEXT,
			device_id TEXT NOT NULL,
			device_name TEXT,
			device_group TEXT,
			timestamp TEXT NOT NULL,
			values_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			sent INTEGER DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_buffer_sent ON buffer(sent);
		CREATE INDEX IF NOT EXISTS idx_buffer_created_at ON buffer(created_at);
	`
	_, err := b.db.Exec(query)
	return err
}

func (b *SQLiteBuffer) Store(ctx context.Context, envelope *model.Envelope) error {
	valuesJSON, err := json.Marshal(envelope.Values)
	if err != nil {
		return fmt.Errorf("failed to marshal values: %w", err)
	}

	query := `
		INSERT INTO buffer (id, station_id, station_name, device_id, device_name, device_group, timestamp, values_json, created_at, sent)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
	`

	_, err = b.db.ExecContext(ctx, query,
		envelope.ID,
		envelope.StationID,
		envelope.StationName,
		envelope.DeviceID,
		envelope.DeviceName,
		envelope.DeviceGroup,
		envelope.Timestamp.Format(time.RFC3339),
		string(valuesJSON),
		time.Now().UTC().Format(time.RFC3339),
	)

	if err != nil {
		return fmt.Errorf("failed to store envelope: %w", err)
	}

	b.log.Debug("envelope stored in buffer", slog.String("id", envelope.ID))
	return nil
}

func (b *SQLiteBuffer) GetPending(ctx context.Context, limit int) ([]*model.Envelope, error) {
	query := `
		SELECT id, station_id, station_name, device_id, device_name, device_group, timestamp, values_json
		FROM buffer
		WHERE sent = 0
		ORDER BY created_at ASC
		LIMIT ?
	`

	rows, err := b.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending envelopes: %w", err)
	}
	defer rows.Close()

	var envelopes []*model.Envelope
	for rows.Next() {
		var (
			id, stationID, stationName, deviceID, deviceName, deviceGroup, timestampStr, valuesJSON string
		)

		if err := rows.Scan(&id, &stationID, &stationName, &deviceID, &deviceName, &deviceGroup, &timestampStr, &valuesJSON); err != nil {
			b.log.Error("failed to scan row", sl.Err(err))
			continue
		}

		timestamp, err := time.Parse(time.RFC3339, timestampStr)
		if err != nil {
			b.log.Error("failed to parse timestamp", sl.Err(err))
			continue
		}

		var values []model.DataPoint
		if err := json.Unmarshal([]byte(valuesJSON), &values); err != nil {
			b.log.Error("failed to unmarshal values", sl.Err(err))
			continue
		}

		envelopes = append(envelopes, &model.Envelope{
			ID:          id,
			StationID:   stationID,
			StationName: stationName,
			DeviceID:    deviceID,
			DeviceName:  deviceName,
			DeviceGroup: deviceGroup,
			Timestamp:   timestamp,
			Values:      values,
		})
	}

	return envelopes, rows.Err()
}

func (b *SQLiteBuffer) MarkSent(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, "DELETE FROM buffer WHERE id = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			return fmt.Errorf("failed to delete envelope %s: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	b.log.Debug("marked envelopes as sent", slog.Int("count", len(ids)))
	return nil
}

func (b *SQLiteBuffer) Cleanup(ctx context.Context, maxAge time.Duration) error {
	cutoff := time.Now().UTC().Add(-maxAge).Format(time.RFC3339)

	result, err := b.db.ExecContext(ctx, "DELETE FROM buffer WHERE created_at < ?", cutoff)
	if err != nil {
		return fmt.Errorf("failed to cleanup old envelopes: %w", err)
	}

	deleted, _ := result.RowsAffected()
	if deleted > 0 {
		b.log.Info("cleaned up old buffer entries", slog.Int64("deleted", deleted))
	}

	return nil
}

func (b *SQLiteBuffer) Close() error {
	return b.db.Close()
}

func (b *SQLiteBuffer) Count(ctx context.Context) (int64, error) {
	var count int64
	err := b.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM buffer WHERE sent = 0").Scan(&count)
	return count, err
}
