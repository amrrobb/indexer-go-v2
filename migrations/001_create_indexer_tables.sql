-- Indexer Tables for Payment Detection
-- These tables track worker positions and provide recovery capability
--
-- USAGE: Run with psql variable for schema name:
--   psql -v indexer_schema=indexer -f migrations/001_create_indexer_tables.sql
--
-- Default schema: 'indexer' (override with -v indexer_schema=your_schema)

-- Set default schema if not provided
\set indexer_schema_default indexer
SELECT COALESCE(:'indexer_schema', :'indexer_schema_default') AS effective_schema \gset

-- Create indexer schema for better organization
CREATE SCHEMA IF NOT EXISTS :effective_schema;

-- Worker position tracking (recovery from failures)
-- This table stores the last processed block number for each worker type
CREATE TABLE IF NOT EXISTS :effective_schema.indexer_cursors (
    id SERIAL PRIMARY KEY,
    chain_id INT NOT NULL,
    worker_type VARCHAR(20) NOT NULL, -- 'forward' | 'backfill'
    last_block BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Ensure one cursor per chain per worker type
    UNIQUE(chain_id, worker_type)
);

-- Index for efficient cursor lookups
CREATE INDEX IF NOT EXISTS idx_indexer_cursors_chain_worker
ON :effective_schema.indexer_cursors(chain_id, worker_type);

-- Optional: Audit trail for processed blocks
-- This table can be used for debugging and analytics
CREATE TABLE IF NOT EXISTS :effective_schema.indexer_processed_blocks (
    id SERIAL PRIMARY KEY,
    chain_id INT NOT NULL,
    block_number BIGINT NOT NULL,
    worker_type VARCHAR(20) NOT NULL,  -- For audit/analytics only
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Prevent duplicate processing of same block (regardless of worker type)
    UNIQUE(chain_id, block_number)
);

-- Index for efficient block lookups
CREATE INDEX IF NOT EXISTS idx_indexer_processed_blocks_chain_block
ON :effective_schema.indexer_processed_blocks(chain_id, block_number);

-- Index for worker type analysis
CREATE INDEX IF NOT EXISTS idx_indexer_processed_blocks_worker_time
ON :effective_schema.indexer_processed_blocks(worker_type, processed_at);

-- Comments for documentation
COMMENT ON SCHEMA :effective_schema IS 'Schema for indexer-specific tables and tracking';
COMMENT ON TABLE :effective_schema.indexer_cursors IS 'Tracks last processed block number for each worker type per chain';
COMMENT ON TABLE :effective_schema.indexer_processed_blocks IS 'Audit trail of processed blocks for debugging and analytics';

-- Grant permissions (adjust based on your database user setup)
-- GRANT SELECT, INSERT, UPDATE ON :effective_schema.indexer_cursors TO indexer_user;
-- GRANT SELECT, INSERT ON :effective_schema.indexer_processed_blocks TO indexer_user;
-- GRANT USAGE ON SCHEMA :effective_schema TO indexer_user;