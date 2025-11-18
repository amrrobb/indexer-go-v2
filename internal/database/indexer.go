package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sirupsen/logrus"
)

// UpsertCursor updates or inserts a cursor record for a worker
func (c *Client) UpsertCursor(ctx context.Context, chainID int, workerType string, lastBlock int64) error {
	query := fmt.Sprintf(`
		INSERT INTO %s.indexer_cursors (chain_id, worker_type, last_block)
		VALUES ($1, $2, $3)
		ON CONFLICT (chain_id, worker_type)
		DO UPDATE SET
			last_block = EXCLUDED.last_block,
			updated_at = now()
	`, c.IndexerSchema)

	cmdTag, err := c.IndexerPool.Exec(ctx, query, chainID, workerType, lastBlock)
	if err != nil {
		return fmt.Errorf("failed to upsert cursor: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		logrus.WithFields(logrus.Fields{
			"chain_id":    chainID,
			"worker_type": workerType,
			"last_block":  lastBlock,
		}).Warn("No rows affected in cursor upsert")
	}

	return nil
}

// GetCursor retrieves the last processed block for a worker
func (c *Client) GetCursor(ctx context.Context, chainID int, workerType string) (int64, error) {
	query := fmt.Sprintf(`
		SELECT last_block
		FROM %s.indexer_cursors
		WHERE chain_id = $1 AND worker_type = $2
	`, c.IndexerSchema)

	var lastBlock int64
	err := c.IndexerPool.QueryRow(ctx, query, chainID, workerType).Scan(&lastBlock)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, nil // No cursor exists yet
		}
		return 0, fmt.Errorf("failed to get cursor: %w", err)
	}

	return lastBlock, nil
}

// MarkBlockProcessed records that a block has been processed
// Note: If block already processed by another worker, this is a no-op (DO NOTHING)
// The worker_type is stored for audit/analytics purposes only
func (c *Client) MarkBlockProcessed(ctx context.Context, chainID int, blockNumber int64, workerType string) error {
	query := fmt.Sprintf(`
		INSERT INTO %s.indexer_processed_blocks (chain_id, block_number, worker_type)
		VALUES ($1, $2, $3)
		ON CONFLICT (chain_id, block_number)
		DO NOTHING
	`, c.IndexerSchema)

	_, err := c.IndexerPool.Exec(ctx, query, chainID, blockNumber, workerType)
	if err != nil {
		return fmt.Errorf("failed to mark block as processed: %w", err)
	}

	return nil
}

// GetLastProcessedBlock finds the highest processed block number for a chain
func (c *Client) GetLastProcessedBlock(ctx context.Context, chainID int) (int64, error) {
	query := fmt.Sprintf(`
		SELECT MAX(block_number)
		FROM %s.indexer_processed_blocks
		WHERE chain_id = $1
	`, c.IndexerSchema)

	var lastBlock sql.NullInt64
	err := c.IndexerPool.QueryRow(ctx, query, chainID).Scan(&lastBlock)
	if err != nil {
		return 0, fmt.Errorf("failed to get last processed block: %w", err)
	}

	if !lastBlock.Valid {
		return 0, nil // No blocks processed yet
	}

	return lastBlock.Int64, nil
}

// IsBlockProcessed checks if a specific block has been processed by any worker
func (c *Client) IsBlockProcessed(ctx context.Context, chainID int, blockNumber int64) (bool, error) {
	query := fmt.Sprintf(`
		SELECT EXISTS(
			SELECT 1
			FROM %s.indexer_processed_blocks
			WHERE chain_id = $1 AND block_number = $2
		)
	`, c.IndexerSchema)

	var exists bool
	err := c.IndexerPool.QueryRow(ctx, query, chainID, blockNumber).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check if block is processed: %w", err)
	}

	return exists, nil
}

// GetWorkerStats returns statistics for a specific worker
func (c *Client) GetWorkerStats(ctx context.Context, chainID int, workerType string) (*WorkerStats, error) {
	cursorQuery := fmt.Sprintf(`
		SELECT last_block, updated_at
		FROM %s.indexer_cursors
		WHERE chain_id = $1 AND worker_type = $2
	`, c.IndexerSchema)

	var stats WorkerStats
	var lastBlock sql.NullInt64
	var updatedAt sql.NullTime

	err := c.IndexerPool.QueryRow(ctx, cursorQuery, chainID, workerType).Scan(&lastBlock, &updatedAt)
	if err != nil && err != pgx.ErrNoRows {
		return nil, fmt.Errorf("failed to get cursor stats: %w", err)
	}

	if lastBlock.Valid {
		stats.LastBlock = lastBlock.Int64
	}
	if updatedAt.Valid {
		stats.UpdatedAt = updatedAt.Time
	}

	// Get processed blocks count in the last hour
	processedQuery := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM %s.indexer_processed_blocks
		WHERE chain_id = $1
			AND worker_type = $2
			AND processed_at >= $3
	`, c.IndexerSchema)

	oneHourAgo := time.Now().Add(-time.Hour)
	err = c.IndexerPool.QueryRow(ctx, processedQuery, chainID, workerType, oneHourAgo).Scan(&stats.BlocksProcessedLastHour)
	if err != nil {
		logrus.WithError(err).Warn("Failed to get blocks processed count")
		stats.BlocksProcessedLastHour = 0
	}

	return &stats, nil
}

// CleanupOldProcessedBlocks removes processed block records older than the specified TTL
func (c *Client) CleanupOldProcessedBlocks(ctx context.Context, ttl time.Duration) error {
	query := fmt.Sprintf(`
		DELETE FROM %s.indexer_processed_blocks
		WHERE processed_at < $1
	`, c.IndexerSchema)

	cutoffTime := time.Now().Add(-ttl)
	cmdTag, err := c.IndexerPool.Exec(ctx, query, cutoffTime)
	if err != nil {
		return fmt.Errorf("failed to cleanup old processed blocks: %w", err)
	}

	if cmdTag.RowsAffected() > 0 {
		logrus.WithFields(logrus.Fields{
			"rows_deleted": cmdTag.RowsAffected(),
			"cutoff_time":  cutoffTime,
		}).Info("Cleaned up old processed blocks")
	}

	return nil
}

// WorkerStats represents statistics for a worker
type WorkerStats struct {
	LastBlock            int64     `json:"last_block"`
	UpdatedAt            time.Time `json:"updated_at"`
	BlocksProcessedLastHour int64   `json:"blocks_processed_last_hour"`
}

// IndexerEvent represents an event that needs to be tracked for confirmations
type IndexerEvent struct {
	ID              string    `json:"id"`
	ChainID         int       `json:"chain_id"`
	TransactionHash string    `json:"transaction_hash"`
	LogIndex        int       `json:"log_index"`
	BlockNumber     int64     `json:"block_number"`
	BlockHash       string    `json:"block_hash"`
	FromAddress     string    `json:"from_address"`
	ToAddress       string    `json:"to_address"`
	TokenAddress    string    `json:"token_address"`
	Amount          string    `json:"amount"`
	TokenSymbol     string    `json:"token_symbol"`
	TokenDecimals   int       `json:"token_decimals"`
	Status          string    `json:"status"`
	Confirmations   int       `json:"confirmations"`
	FirstDetectedAt time.Time `json:"first_detected_at"`
	ConfirmedAt     *time.Time `json:"confirmed_at,omitempty"`
	WebhookSent     bool      `json:"webhook_sent"`
	WebhookSentAt   *time.Time `json:"webhook_sent_at,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// UpdateStats updates worker statistics
func (c *Client) UpdateStats(ctx context.Context, chainID int, workerType string, blocksProcessed int64, eventsDetected int, eventsConfirmed int) error {
	query := fmt.Sprintf(`
		INSERT INTO %s.indexer_stats (chain_id, worker_type, blocks_processed, events_detected, events_confirmed, last_activity)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (chain_id, worker_type)
		DO UPDATE SET
			blocks_processed = %s.indexer_stats.blocks_processed + EXCLUDED.blocks_processed,
			events_detected = %s.indexer_stats.events_detected + EXCLUDED.events_detected,
			events_confirmed = %s.indexer_stats.events_confirmed + EXCLUDED.events_confirmed,
			last_activity = EXCLUDED.last_activity
	`, c.IndexerSchema, c.IndexerSchema, c.IndexerSchema, c.IndexerSchema)

	_, err := c.IndexerPool.Exec(ctx, query, chainID, workerType, blocksProcessed, eventsDetected, eventsConfirmed)
	if err != nil {
		return fmt.Errorf("failed to update stats: %w", err)
	}

	return nil
}

// SaveEvent saves or updates an indexer event
func (c *Client) SaveEvent(ctx context.Context, event *IndexerEvent) error {
	// For now, we'll use Redis for event storage as it's more suitable for this use case
	// This method can be extended to use database if needed for persistence
	logrus.WithFields(logrus.Fields{
		"event_id": event.ID,
		"chain_id": event.ChainID,
		"status":   event.Status,
	}).Debug("Event saved to Redis (DB persistence not implemented yet)")

	return nil
}