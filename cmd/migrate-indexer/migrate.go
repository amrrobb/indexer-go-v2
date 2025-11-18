package main

import (
	"context"
	"fmt"
	"log"

	"github.com/joho/godotenv"
	"indexer-go-v2/internal/database"
)

func main() {
	// Load .env
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: Could not load .env file: %v", err)
	}

	// Connect to database
	db, err := database.NewClient()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Create indexer schema and tables
	ctx := context.Background()

	log.Printf("Using indexer schema: %s", db.IndexerSchema)

	// Create schema
	createSchemaSQL := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;", db.IndexerSchema)
	_, err = db.IndexerPool.Exec(ctx, createSchemaSQL)
	if err != nil {
		log.Fatalf("Failed to create indexer schema: %v", err)
	}

	// Create indexer_cursors table
	cursorSQL := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %s.indexer_cursors (
		id SERIAL PRIMARY KEY,
		chain_id INTEGER NOT NULL,
		worker_type VARCHAR(20) NOT NULL,
		last_block BIGINT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE(chain_id, worker_type)
	);
	`, db.IndexerSchema)

	_, err = db.IndexerPool.Exec(ctx, cursorSQL)
	if err != nil {
		log.Fatalf("Failed to create indexer_cursors table: %v", err)
	}

	// Create index for indexer_cursors
	cursorIndexSQL := fmt.Sprintf(`
	CREATE INDEX IF NOT EXISTS idx_indexer_cursors_chain_worker
	ON %s.indexer_cursors(chain_id, worker_type);
	`, db.IndexerSchema)

	_, err = db.IndexerPool.Exec(ctx, cursorIndexSQL)
	if err != nil {
		log.Fatalf("Failed to create indexer_cursors index: %v", err)
	}

	// Create indexer_processed_blocks table
	// Note: UNIQUE on (chain_id, block_number) prevents duplicate block processing
	// worker_type is kept for audit/analytics purposes only
	blocksSQL := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %s.indexer_processed_blocks (
		id SERIAL PRIMARY KEY,
		chain_id INTEGER NOT NULL,
		block_number BIGINT NOT NULL,
		worker_type VARCHAR(20) NOT NULL,
		processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE(chain_id, block_number)
	);
	`, db.IndexerSchema)

	_, err = db.IndexerPool.Exec(ctx, blocksSQL)
	if err != nil {
		log.Fatalf("Failed to create indexer_processed_blocks table: %v", err)
	}

	// Create indexes for indexer_processed_blocks
	blocksIndexSQL := fmt.Sprintf(`
	CREATE INDEX IF NOT EXISTS idx_indexer_processed_blocks_chain_block
	ON %s.indexer_processed_blocks(chain_id, block_number);

	CREATE INDEX IF NOT EXISTS idx_indexer_processed_blocks_worker_time
	ON %s.indexer_processed_blocks(worker_type, processed_at);
	`, db.IndexerSchema, db.IndexerSchema)

	_, err = db.IndexerPool.Exec(ctx, blocksIndexSQL)
	if err != nil {
		log.Fatalf("Failed to create indexer_processed_blocks indexes: %v", err)
	}

	// Create indexer_stats table
	statsSQL := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %s.indexer_stats (
		id SERIAL PRIMARY KEY,
		chain_id INTEGER NOT NULL,
		worker_type VARCHAR(20) NOT NULL,
		blocks_processed BIGINT DEFAULT 0,
		events_detected INTEGER DEFAULT 0,
		events_confirmed INTEGER DEFAULT 0,
		error_count INTEGER DEFAULT 0,
		last_activity TIMESTAMPTZ DEFAULT NOW(),
		created_at TIMESTAMPTZ DEFAULT NOW(),
		updated_at TIMESTAMPTZ DEFAULT NOW(),
		UNIQUE(chain_id, worker_type)
	);
	`, db.IndexerSchema)

	_, err = db.IndexerPool.Exec(ctx, statsSQL)
	if err != nil {
		log.Fatalf("Failed to create indexer_stats table: %v", err)
	}

	// Create index for indexer_stats
	statsIndexSQL := fmt.Sprintf(`
	CREATE INDEX IF NOT EXISTS idx_indexer_stats_chain_worker
	ON %s.indexer_stats(chain_id, worker_type);
	`, db.IndexerSchema)

	_, err = db.IndexerPool.Exec(ctx, statsIndexSQL)
	if err != nil {
		log.Fatalf("Failed to create indexer_stats index: %v", err)
	}

	// Add comments for documentation
	commentsSQL := fmt.Sprintf(`
	COMMENT ON SCHEMA %s IS 'Schema for indexer-specific tables and tracking';
	COMMENT ON TABLE %s.indexer_cursors IS 'Tracks last processed block number for each worker type per chain';
	COMMENT ON TABLE %s.indexer_processed_blocks IS 'Audit trail of processed blocks for debugging and analytics';
	COMMENT ON TABLE %s.indexer_stats IS 'Statistics table for monitoring indexer performance';
	`, db.IndexerSchema, db.IndexerSchema, db.IndexerSchema, db.IndexerSchema)

	_, err = db.IndexerPool.Exec(ctx, commentsSQL)
	if err != nil {
		log.Printf("Warning: Failed to add comments (non-critical): %v", err)
	}

	log.Println("✅ Indexer schema and tables created successfully!")
	log.Println("📊 Tables created:")
	log.Printf("  - %s.indexer_cursors", db.IndexerSchema)
	log.Printf("  - %s.indexer_processed_blocks", db.IndexerSchema)
	log.Printf("  - %s.indexer_stats", db.IndexerSchema)
}