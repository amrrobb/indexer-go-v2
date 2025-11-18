# Startup Guide

## 🚀 How to Run the Indexer

### Prerequisites
1. **PostgreSQL**: `localhost:15432` (from appreal-infra)
2. **Redis**: `localhost:6379` (from appreal-infra)
3. **ERPC**: `localhost:4000` (from appreal-erpc)
4. **Backend**: `localhost:8000` (your backend API)

### Quick Start

```bash
# 1. Check infrastructure health
make health

# 2. Run database migrations (first time)
make migrate-existing

# 3. Start all workers (Ethereum + Polygon)
make existing

# 4. Monitor logs
make logs
```

## 👥 Worker Architecture

### Three Worker Types Per Chain

1. **Forward Worker** - Real-time processing
   - Starts from current block
   - Processes new blocks as they're mined
   - Runs every 3 seconds

2. **Backfill Worker** - Gap filling
   - Processes historical blocks
   - Fills gaps from downtime
   - Runs every 30 seconds

3. **Confirmation Worker** - Transaction finality
   - Checks pending transactions
   - Sends confirmed webhooks
   - Runs every 30 seconds

### Running Workers Separately

**Option 1: Single Worker**
```bash
# Ethereum forward worker only
go run cmd/forward/main.go --chain=1

# Polygon backfill worker only
go run cmd/backfill/main.go --chain=137

# Ethereum confirmation worker only
go run cmd/confirmation/main.go --chain=1
```

**Option 2: All Workers for One Chain**
```bash
# All Ethereum workers
make start-chain chain=ethereum

# All Polygon workers
make start-chain chain=polygon
```

**Option 3: All Workers (Recommended)**
```bash
# All workers for all chains
make existing
```

## 📊 Worker Status Monitoring

### Check Worker Health
```bash
# Check all workers
make status

# Check specific chain
curl http://localhost:8080/health?chain=1
```

### Sample Status Response
```json
{
  "healthy": true,
  "workers": {
    "ethereum": {
      "forward": {
        "last_block": 18500001,
        "blocks_processed_last_hour": 1200,
        "events_detected": 45
      },
      "backfill": {
        "last_block": 18450000,
        "blocks_processed_last_hour": 5000,
        "events_detected": 180
      },
      "confirmation": {
        "last_block": 18499900,
        "blocks_processed_last_hour": 1150,
        "events_confirmed": 42
      }
    },
    "polygon": {
      "forward": {
        "last_block": 52000001,
        "blocks_processed_last_hour": 800,
        "events_detected": 25
      }
    }
  }
}
```

## 🔧 Configuration

### Environment Variables
```bash
# Database pools
CONFIG_POOL_MAX_CONNS=5      # Backend reads
CONFIG_POOL_MIN_CONNS=2
INDEXER_POOL_MAX_CONNS=10    # Indexer writes
INDEXER_POOL_MIN_CONNS=3

# Worker timing
FORWARD_INTERVAL=3s          # Forward worker frequency
BACKFILL_INTERVAL=30s        # Backfill worker frequency
CONFIRMATION_INTERVAL=30s    # Confirmation worker frequency
```

### Chain-Specific Settings
```bash
# Required confirmations per chain
ETHEREUM_CONFIRMATIONS=12    # ~3 minutes
POLYGON_CONFIRMATIONS=20     # ~5 minutes
ARBITRUM_CONFIRMATIONS=15    # ~3 minutes
BSC_CONFIRMATIONS=10         # ~3 minutes
```

## 📝 First Time Setup

### 1. Database Setup
```bash
# Create indexer schema (automatically done by migrations)
make migrate-existing

# Verify tables created
psql postgresql://postgres:password@localhost:15432/postgres -c "\dt indexer.*"
```

### 2. Backend Data Required
Make sure your backend has:

**Networks (public.networks):**
```sql
INSERT INTO networks (id, network_id, network, blockchain_network_type, is_active)
VALUES
  (1, 1, 'ethereum', 'EVM', true),
  (2, 137, 'polygon', 'EVM', true);
```

**Currencies (public.currencies):**
```sql
INSERT INTO currencies (id, network_id, contract_address, symbol, decimals, is_active, is_used_for_payment)
VALUES
  ('usdc-eth', 'ethereum', '0xA0b86a33E6441a6c8c0f4c8c4d0F4A3B9C0B0B0B', 'USDC', 6, true, true),
  ('usdt-poly', 'polygon', '0xc2132D05D31c914a87C6611C10748AEb04B58e8F', 'USDT', 6, true, true);
```

**Wallets (public.wallets):**
```sql
INSERT INTO wallets (id, network_id, address, type, is_active)
VALUES
  ('wallet-1', 'ethereum', '0x1234567890123456789012345678901234567890', 'DEPOSIT', true),
  ('wallet-2', 'polygon', '0x1234567890123456789012345678901234567890', 'DEPOSIT', true);
```

### 3. Test Webhook Endpoint
Your backend should accept:
```http
POST /webhooks/payments
Content-Type: application/json
X-Indexer-Signature: sha256=...

{
  "event_type": "payment_detected",
  "chain_id": 1,
  "transaction_hash": "0x...",
  "log_index": 5,
  "block_number": 18500001,
  "from_address": "0x...",
  "to_address": "0x...",  // Your deposit wallet
  "token_address": "0x...",
  "amount": "1000000",    // With decimals
  "symbol": "USDC",
  "status": "detected"    // → "confirmed"
}
```

## 🚨 Troubleshooting

### Common Issues

**Database Connection:**
```bash
# Test connection
psql postgresql://postgres:password@localhost:15432/postgres -c "SELECT 1;"

# Check tables exist
psql postgresql://postgres:password@localhost:15432/postgres -c "\dt indexer.*"
```

**Redis Connection:**
```bash
# Test Redis
redis-cli -h localhost -p 6379 -a admin ping

# Check Redis is accessible
redis-cli -h localhost -p 6379 -a admin set test "hello"
```

**ERPC Connection:**
```bash
# Test ERPC health
curl http://localhost:4000/health

# Test specific chain
curl http://localhost:4000/ethereum/1 \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
```

### Worker Not Starting?

1. **Check logs:**
   ```bash
   make logs
   ```

2. **Verify configuration:**
   ```bash
   # Check database schema
   psql postgresql://postgres:password@localhost:15432/postgres -c "SELECT schema_name FROM information_schema.schemata WHERE schema_name = 'indexer';"

   # Check backend data
   psql postgresql://postgres:password@localhost:15432/postgres -c "SELECT COUNT(*) FROM networks WHERE is_active = true AND blockchain_network_type = 'EVM';"
   ```

3. **Check environment:**
   ```bash
   # Verify .env file
   cat .env | grep -E "(DATABASE_URL|REDIS_|ETHEREUM_RPC_URL)"
   ```

## 📈 Performance Tips

### Database Optimization
- Use provided connection pool settings
- Monitor `IndexerPool` for indexer operations
- Monitor `ConfigPool` for backend reads

### Worker Optimization
- Forward worker: Keep `FORWARD_INTERVAL` low (3s)
- Backfill worker: Adjust `BACKFILL_WINDOW_SIZE` based on performance
- Confirmation worker: Batch confirmations for efficiency

### Redis Usage
- Redis TTL automatically expires old data
- Monitor Redis memory usage during initial sync
- Workers continue without Redis (using PostgreSQL backup)