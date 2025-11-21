# Config Auto-Reload Feature

## Overview

The config auto-reload feature allows workers to automatically detect and reload configuration changes when wallets or currencies are added, updated, or removed from the database - **without requiring a restart**.

This is implemented using PostgreSQL's **LISTEN/NOTIFY** mechanism, which provides:
- ✅ **Zero polling overhead** - no redundant database queries
- ✅ **Real-time notifications** - changes detected instantly
- ✅ **Event-driven architecture** - only reloads when actual changes occur
- ✅ **Debouncing** - groups rapid changes into a single reload (2-second window)

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  Database (appreal_dev schema)                                  │
│                                                                  │
│  ┌──────────┐   INSERT/UPDATE/DELETE    ┌──────────────────┐   │
│  │ wallets  │ ─────────────────────────> │ Database Trigger │   │
│  └──────────┘                            │  notify_config_  │   │
│                                           │     change()     │   │
│  ┌─────────────┐ INSERT/UPDATE/DELETE    │                  │   │
│  │ currencies  │ ─────────────────────> │  NOTIFY channel: │   │
│  └─────────────┘                          │  'config_change' │   │
│                                           └────────┬─────────┘   │
└────────────────────────────────────────────────────┼─────────────┘
                                                     │
                                                     │ pg_notify()
                                                     ▼
                                          ┌──────────────────────┐
                                          │  ConfigChangeListener│
                                          │  (Go Process)        │
                                          │                      │
                                          │  - LISTEN connection │
                                          │  - Debouncing (2s)   │
                                          │  - Reload queue      │
                                          └──────────┬───────────┘
                                                     │
                                                     │ TriggerConfigReload()
                                                     ▼
                                          ┌──────────────────────┐
                                          │  ForwardWorker       │
                                          │                      │
                                          │  - Reload config     │
                                          │  - Continue processing│
                                          └──────────────────────┘
```

## Setup Instructions

### 1. Apply Database Migration

The migration is located in the **appreal-backend** repository:

**Migration Location**: `appreal-backend/prisma/migrations/20251120000000_add_config_change_notifications/migration.sql`

```bash
# Option 1: Apply using Prisma (recommended)
cd /Users/ammar/Documents/Webtiga/appreal-backend
npx prisma migrate deploy

# Option 2: Apply manually with psql
cd /Users/ammar/Documents/Webtiga/appreal-backend
psql -U ammar.robb -d appreal \
  -f prisma/migrations/20251120000000_add_config_change_notifications/migration.sql
```

This will create:
- `notify_config_change()` function
- Triggers on `"wallets"` table (INSERT, UPDATE, DELETE)
- Triggers on `"currencies"` table (INSERT, UPDATE, DELETE)

**Note**: The migration uses Prisma's quoted table names (`"wallets"`, `"currencies"`).

### 2. Enable Config Auto-Reload

Set the environment variable in your `.env` or `.env.docker` file:

```bash
# Enable auto-reload
CONFIG_AUTO_RELOAD=true
```

**Note**: This is already enabled by default in `.env` and `.env.docker`.

### 3. Rebuild and Restart Workers

```bash
# For Docker deployment
docker-compose -f docker-compose.multi-chain.yml down
docker-compose -f docker-compose.multi-chain.yml build --no-cache
docker-compose -f docker-compose.multi-chain.yml up -d

# Check logs to confirm auto-reload is enabled
docker logs indexer-forward-ethereum | grep "auto-reload"
```

You should see:
```
INFO Config auto-reload is ENABLED - will reload on database changes
INFO Subscribed to config_change notifications
```

## How It Works

### Database Triggers

When relevant changes occur, triggers fire and send notifications:

```sql
-- Example: Adding a new active merchant wallet
INSERT INTO wallets (network_id, address, type, status)
SELECT id, '0xNEWWALLET...', 'MERCHANT_WALLET', 'ACTIVE'
FROM networks WHERE chain_id = 1;

-- Trigger fires → NOTIFY 'config_change' with JSON payload:
{
  "table": "wallets",
  "operation": "INSERT",
  "network_id": "uuid-for-ethereum",
  "timestamp": "2025-01-20T10:30:45.123Z"
}
```

### Listener & Debouncing

The `ConfigChangeListener` (in [internal/config/listener.go](../internal/config/listener.go)):

1. **Receives notification** via PostgreSQL LISTEN connection
2. **Queues the change** in a reload channel
3. **Waits 2 seconds** for additional changes (debouncing)
4. **Reloads configuration** once after debounce period
5. **Triggers worker reload** via channel

### Worker Reload

The `ForwardWorker` (in [internal/worker/forward.go](../internal/worker/forward.go)):

1. **Receives reload signal** via `reloadChannel`
2. **Fetches fresh config** from database
3. **Swaps to new config** atomically
4. **Continues processing** with updated wallets/currencies

## Testing

### Manual Testing

1. **Start a worker** and watch its logs:
```bash
docker logs -f indexer-forward-ethereum
```

2. **In another terminal, add a new wallet**:
```sql
-- Connect to your database
psql -U ammar.robb -d appreal

-- Add a new active merchant wallet (using Prisma table names with quotes)
INSERT INTO "wallets" (id, network_id, address, type, status, version, wallet_provider, wallet_ownership, created_by, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  (SELECT id FROM "networks" WHERE chain_id = 1),
  '0xTESTWALLET123',
  'MERCHANT_WALLET',
  'ACTIVE',
  'V1',
  'PRIVY',
  'INTERNAL',
  'system',
  NOW(),
  NOW()
);
```

3. **Expected output in worker logs**:
```
INFO Received config change notification table=wallets operation=INSERT network_id=uuid-for-ethereum
INFO Config change queued, waiting for more changes pending_count=1
INFO Debounce period elapsed, reloading configuration change_count=1
INFO 🔄 Reloading worker configuration...
INFO ✅ Configuration reloaded successfully active_currencies=3 watched_addresses=5
```

### Testing Currency Changes

```sql
-- Activate a previously inactive currency (using Prisma table names)
UPDATE "currencies"
SET is_active = true, is_used_for_payment = true
WHERE symbol = 'USDT' AND network_id = (SELECT id FROM "networks" WHERE chain_id = 1);
```

Expected output:
```
INFO Received config change notification table=currencies operation=UPDATE network_id=uuid-for-ethereum
INFO 🔄 Reloading worker configuration...
INFO ✅ Configuration reloaded successfully active_currencies=4 watched_addresses=5
```

### Testing with psql LISTEN

You can monitor notifications directly:

```sql
-- Terminal 1: Listen for notifications
LISTEN config_change;

-- You'll see notifications when changes occur
-- Example output:
-- Asynchronous notification "config_change" with payload "{...}" received from server process...
```

```sql
-- Terminal 2: Make a change (using Prisma table names)
UPDATE "wallets" SET status = 'INACTIVE' WHERE address = '0x123...';
```

## Disabling Auto-Reload

To disable auto-reload (requires manual restart for config changes):

```bash
# In .env or .env.docker
CONFIG_AUTO_RELOAD=false
```

Workers will log:
```
WARN Config auto-reload is DISABLED - workers must be restarted to detect wallet/currency changes
```

## Troubleshooting

### No notifications received

1. **Check migration was applied**:
```sql
-- Check if triggers exist
SELECT trigger_name, event_object_table
FROM information_schema.triggers
WHERE trigger_name LIKE '%config_change%';

-- Should return 6 rows (3 for wallets, 3 for currencies)
```

2. **Check LISTEN connection**:
```bash
# Look for "Subscribed to config_change notifications" in logs
docker logs indexer-forward-ethereum | grep "config_change"
```

3. **Verify migration was applied**:
```sql
-- Check that the function exists
SELECT routine_name FROM information_schema.routines
WHERE routine_name = 'notify_config_change';

-- Check that triggers exist (should return 6 rows)
SELECT trigger_name, event_object_table
FROM information_schema.triggers
WHERE trigger_name LIKE '%config_change%';
```

### Config not reloading

1. **Check CONFIG_AUTO_RELOAD is enabled**:
```bash
docker exec indexer-forward-ethereum env | grep CONFIG_AUTO_RELOAD
# Should show: CONFIG_AUTO_RELOAD=true
```

2. **Verify triggers are firing**:
```sql
-- Add debug logging to trigger function
CREATE OR REPLACE FUNCTION notify_config_change()
RETURNS trigger AS $$
BEGIN
    RAISE NOTICE 'Trigger fired: % % %', TG_TABLE_NAME, TG_OP, COALESCE(NEW.id, OLD.id);
    -- ... rest of function
END;
$$ LANGUAGE plpgsql;
```

3. **Check debounce timing**:
- Changes are grouped with a 2-second delay
- Look for "Debounce period elapsed" in logs after changes

## Performance Impact

- **Idle workers**: Zero overhead (no polling)
- **During reload**: Single database query to fetch new config
- **LISTEN connection**: Uses 1 dedicated connection per worker
- **Memory**: Minimal (~100 bytes per queued notification)

## Alternative: Version Tracking

If LISTEN/NOTIFY doesn't work in your environment, an alternative version tracking approach using a `config_version` table with incrementing version numbers could be implemented. Workers would need to periodically check for version changes (requires polling).

**Note**: This would need to be created as a new Prisma migration in appreal-backend/prisma/migrations.

## Files Modified

### New Files

**In appreal-backend repository:**
- `prisma/migrations/20251120000000_add_config_change_notifications/migration.sql` - Database triggers
- `prisma/scripts/deploy-with-data-migrations.ts` - Enhanced to handle PL/pgSQL dollar-quoting

**In indexer-go-v2 repository:**
- [internal/config/listener.go](../internal/config/listener.go) - LISTEN/NOTIFY handler
- [docs/CONFIG_AUTO_RELOAD.md](./CONFIG_AUTO_RELOAD.md) - This documentation

### Modified Files

**In indexer-go-v2 repository:**
- [cmd/forward/main.go](../cmd/forward/main.go) - Listener integration for forward workers
- [internal/worker/forward.go](../internal/worker/forward.go) - Forward worker reload channel
- [internal/worker/backfill.go](../internal/worker/backfill.go) - Backfill worker reload channel
- [internal/worker/runner.go](../internal/worker/runner.go) - Shared listener for backfill/confirmation workers
- [.env](../.env) - CONFIG_AUTO_RELOAD variable
- [.env.docker](../.env.docker) - CONFIG_AUTO_RELOAD variable
- [migrations/README.md](../migrations/README.md) - Updated to clarify migration management

## Supported Workers

Config auto-reload is currently supported for:
- ✅ **Forward workers** - Real-time block processing
- ✅ **Backfill workers** - Historical block processing
- ❌ **Confirmation workers** - Not yet implemented

## Future Enhancements

Potential improvements:
- [ ] Apply to confirmation workers
- [ ] Network-specific reloads (only reload affected chain)
- [ ] Metrics tracking (reload count, duration, errors)
- [ ] Graceful handling of in-flight transactions during reload
- [ ] Configuration validation before applying changes
- [ ] Admin API to trigger manual reload

## Schema Compatibility

The Prisma migration in appreal-backend uses quoted table names (`"wallets"`, `"currencies"`), which works with Prisma's schema-agnostic approach. No need to set `search_path` when using `npx prisma migrate deploy`.

For manual psql execution, Prisma migrations work in the default schema configured in your `DATABASE_URL`.
