# Migrations Directory

## Overview

This directory contains **indexer-specific migrations** for the indexer-go-v2 project. These migrations are for the `indexer` schema only.

## Important Notes

### Backend Schema Migrations

All migrations affecting the **backend schema** (wallets, currencies, networks, etc.) are managed through the **appreal-backend** repository using Prisma:

- **Location**: `appreal-backend/prisma/migrations/`
- **Applied via**: `npx prisma migrate deploy`
- **Examples**: Config change notifications, wallet/currency schema changes

### Migrations in This Directory

The migrations here are:
- ✅ **Indexer schema only** - for `indexer` schema tables (processed_blocks, etc.)
- ✅ **Manual application** - must be run manually with psql
- ⚠️ **Not managed by Prisma** - these are standalone SQL scripts

## Available Migrations

### 001_create_indexer_tables.sql
Creates core indexer tables:
- `processed_blocks` - Track processed block ranges per chain
- Other indexer-specific tables

### 002_add_processed_blocks.sql
Adds additional columns or indexes to processed_blocks table.

## Config Auto-Reload Feature

The config auto-reload feature (PostgreSQL LISTEN/NOTIFY) is implemented in the backend schema and managed through appreal-backend:

**Location**: `appreal-backend/prisma/migrations/20251120000000_add_config_change_notifications/`

**Documentation**: See [docs/CONFIG_AUTO_RELOAD.md](../docs/CONFIG_AUTO_RELOAD.md)

**To apply**:
```bash
cd /path/to/appreal-backend
npx prisma migrate deploy
```

## When to Use Prisma vs Manual Migrations

### Use Prisma Migrations (appreal-backend)
- ✅ Backend schema changes (wallets, currencies, networks)
- ✅ Production environments
- ✅ Need version control and rollback
- ✅ Triggers and functions affecting backend tables

### Use Manual Migrations (this directory)
- ✅ Indexer schema changes (processed_blocks, etc.)
- ✅ Development/testing
- ✅ Custom indexer-specific tables

## Applying Indexer Migrations

```bash
# Apply to indexer schema
psql -U ammar.robb -d appreal <<EOF
SET search_path TO indexer;
\i migrations/001_create_indexer_tables.sql
\i migrations/002_add_processed_blocks.sql
EOF
```

## See Also

- [Config Auto-Reload Documentation](../docs/CONFIG_AUTO_RELOAD.md)
- [appreal-backend Prisma Migrations](../../appreal-backend/prisma/migrations/)
