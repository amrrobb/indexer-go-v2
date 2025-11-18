# Arbitrum Worker Startup Guide

## 🚀 Run Arbitrum Workers

### Prerequisites
- PostgreSQL running on localhost:15432
- Redis running on localhost:6379
- ERPC running on localhost:4000

### 1. Forward Worker (Real-time)
```bash
# Arbitrum forward worker
go run cmd/forward/main.go --chain=42161

# Or using chain name
go run cmd/forward/main.go --chain=arbitrum
```

### 2. Backfill Worker (Historical)
```bash
# Arbitrum backfill worker
go run cmd/backfill/main.go --chain=42161

# Or using chain name
go run cmd/backfill/main.go --chain=arbitrum
```

### 3. Both Workers Together
```bash
# Terminal 1: Forward worker
go run cmd/forward/main.go --chain=42161

# Terminal 2: Backfill worker
go run cmd/backfill/main.go --chain=42161

# Terminal 3: Confirmation worker
go run cmd/confirmation/main.go --chain=42161
```

## 🔍 Monitoring Webhook Processing

### Redis Monitoring (Primary)

**Redis Keys Used:**
```
events:detected          # New events waiting for confirmation
events:confirmed         # Confirmed events
events:dedupe           # Deduplication set (prevents duplicates)
webhook:failed:<tx_hash> # Failed webhook deliveries
cursor:forward:<chain>  # Forward worker position
cursor:backfill:<chain> # Backfill worker position
```

**Check with Redis CLI:**
```bash
# Connect to Redis
redis-cli -h localhost -p 6379 -a admin

# Check for detected events
LRANGE events:detected 0 -1

# Check confirmed events
LRANGE events:confirmed 0 -1

# Check deduplication set
SMEMBERS events:dedupe

# Check worker positions
GET cursor:forward:42161
GET cursor:backfill:42161

# Check webhook failures
KEYS webhook:failed:*
```

**Redis Insight Usage:**
- ✅ Yes! You can use Redis Insight to monitor these keys
- Connect: `localhost:6379` with password `admin`
- Watch these key patterns in real-time

### Webhook Endpoint Monitoring

**Your Backend (localhost:8000):**
```bash
# Check webhook endpoint logs
tail -f /path/to/your/backend/logs | grep "webhook"

# Or monitor HTTP requests
curl -X POST http://localhost:8000/webhooks/payments \
  -H "Content-Type: application/json" \
  -H "X-Indexer-Signature: test" \
  -d '{"test": "connection"}'
```

### Database Monitoring

**Check indexer tables:**
```sql
-- Worker positions
SELECT * FROM indexer.indexer_cursors WHERE chain_id = 42161;

-- Processed blocks
SELECT * FROM indexer.indexer_processed_blocks
WHERE chain_id = 42161
ORDER BY processed_at DESC LIMIT 10;

-- Worker stats
SELECT * FROM indexer.indexer_stats
WHERE chain_id = 42161;
```

## 📊 Expected Behavior

### Forward Worker
- Starts from current Arbitrum block
- Processes new blocks every 3 seconds
- **Watches 1,000+ wallets automatically** with smart batching
- Sends "detected" webhooks immediately
- **Optimized for ERPC**: 100 wallets per call, 5 parallel calls

### Backfill Worker
- Processes historical blocks ( configurable start)
- Runs every 30 seconds
- Fills gaps from downtime
- Also sends "detected" webhooks

### Confirmation Worker
- Checks detected events for confirmations
- Arbitrum needs ~10 confirmations (~10 seconds)
- Sends "confirmed" webhooks when ready

## 🚨 Troubleshooting

### No Events Detected?
```bash
# Check if wallets are configured
psql postgresql://postgres:password@localhost:15432/postgres -c "
SELECT w.address, w.network_id
FROM wallets w
WHERE w.network_id = 'arbitrum' AND w.type = 'DEPOSIT' AND w.is_active = true;
"

# Check if currencies are configured
psql postgresql://postgres:password@localhost:15432/postgres -c "
SELECT c.symbol, c.contract_address
FROM currencies c
WHERE c.network_id = 'arbitrum' AND c.is_used_for_payment = true;
"
```

### Webhook Not Receiving?
```bash
# Check Redis for stuck events
redis-cli -h localhost -p 6379 -a admin LRANGE events:detected 0 -1

# Check for webhook failures
redis-cli -h localhost -p 6379 -a admin KEYS webhook:failed:*

# Test webhook endpoint manually
curl -v http://localhost:8000/webhooks/payments
```

### Worker Stuck?
```bash
# Check worker positions
redis-cli -h localhost -p 6379 -a admin GET cursor:forward:42161

# Check database cursor
psql postgresql://postgres:password@localhost:15432/postgres -c "
SELECT last_block, worker_type, updated_at
FROM indexer.indexer_cursors
WHERE chain_id = 42161;
"
```

## 🎯 Quick Test

1. **Start workers:**
   ```bash
   go run cmd/forward/main.go --chain=42161 &
   go run cmd/backfill/main.go --chain=42161 &
   ```

2. **Monitor Redis:**
   ```bash
   redis-cli -h localhost -p 6379 -a admin
   > MONITOR
   ```

3. **Check for events:**
   ```bash
   redis-cli -h localhost -p 6379 -a admin LRANGE events:detected 0 10
   ```

4. **Monitor webhooks:**
   Watch your backend logs for incoming webhook requests

The workers will start processing immediately if your backend data is configured correctly!