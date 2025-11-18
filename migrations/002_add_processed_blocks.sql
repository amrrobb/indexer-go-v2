-- Add indexer_stats table for monitoring
-- This migration adds the stats table to the indexer schema
--
-- USAGE: Run with psql variable for schema name:
--   psql -v indexer_schema=indexer -f migrations/002_add_processed_blocks.sql
--
-- Default schema: 'indexer' (override with -v indexer_schema=your_schema)

-- Set default schema if not provided
\set indexer_schema_default indexer
SELECT COALESCE(:'indexer_schema', :'indexer_schema_default') AS effective_schema \gset

-- Add indexer stats table for monitoring
CREATE TABLE IF NOT EXISTS :effective_schema.indexer_stats (
    id SERIAL PRIMARY KEY,
    chain_id INTEGER NOT NULL,
    worker_type VARCHAR(20) NOT NULL,
    blocks_processed BIGINT DEFAULT 0,
    events_detected INTEGER DEFAULT 0,
    events_confirmed INTEGER DEFAULT 0,
    error_count INTEGER DEFAULT 0,
    last_activity TIMESTAMP DEFAULT NOW(),

    -- One stats entry per chain per worker type
    UNIQUE(chain_id, worker_type)
);

CREATE INDEX IF NOT EXISTS idx_indexer_stats_chain_worker ON :effective_schema.indexer_stats(chain_id, worker_type);

COMMENT ON TABLE :effective_schema.indexer_stats IS 'Statistics table for monitoring indexer performance';
COMMENT ON COLUMN :effective_schema.indexer_stats.blocks_processed IS 'Total blocks processed by this worker';
COMMENT ON COLUMN :effective_schema.indexer_stats.events_detected IS 'Total payment events detected by this worker';
COMMENT ON COLUMN :effective_schema.indexer_stats.events_confirmed IS 'Total payment events confirmed by this worker';
COMMENT ON COLUMN :effective_schema.indexer_stats.error_count IS 'Number of errors encountered by this worker';