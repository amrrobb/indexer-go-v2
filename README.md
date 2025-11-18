# indexer-go-v2 - Simple Multi-Chain Payment Indexer

A lightweight, high-performance Go indexer for detecting ERC-20 token transfers to watched addresses across multiple EVM chains. Designed to integrate seamlessly with existing backend infrastructure.

## 📚 Documentation

- **[Architecture Overview](./docs/01-ARCHITECTURE.md)** - System design and worker architecture
- **[Confirmation Mechanism](./docs/02-CONFIRMATION.md)** - How transactions are confirmed
- **[Data Processing](./docs/03-DATA-PROCESSING.md)** - What gets stored where
- **[Setup Guide](./docs/04-SETUP.md)** - Quick start with existing infrastructure
- **[Startup Guide](./docs/05-STARTUP.md)** - How to run workers and monitor
- **[Sample Data](./docs/SAMPLE_DATA.md)** - Example data stored by indexer

## 🚀 Quick Start

```bash
# 1. Check existing infrastructure is running
make health

# 2. Run database migrations (first time)
make migrate-existing

# 3. Start workers (Ethereum + Polygon)
make existing

# 4. Monitor logs
make logs
```

## 📋 What This Does

**indexer-go-v2** is a payment processing engine that:

1. **Detects** ERC20 transfers to watched addresses
2. **Schedules** confirmation checks based on chain finality
3. **Confirms** transactions when they reach required confirmations
4. **Sends** webhooks to your backend (detected → confirmed)
5. **Avoids** duplicates through Redis coordination
6. **Recovers** from failures using PostgreSQL

## 🏗️ System Design

- **No Genesis Sync**: Starts from current block, no historical processing burden
- **Triple Workers**: Forward (real-time) + Backfill (gaps) + Confirmation (finality)
- **Existing Infrastructure**: Uses your PostgreSQL, Redis, and ERPC
- **Hybrid Storage**: Redis for speed + PostgreSQL for recovery
- **Backend Integration**: Webhook-based payment status updates

## 🔄 Worker Types & Webhook Flow

### Forward Worker (Real-time)
**Purpose**: Catches new transactions as they happen
**Frequency**: Every 5 seconds
**Range**: Last 10 blocks (with confirmations)

```bash
# Run forward worker
go run cmd/forward/main.go --chain=42161  # Arbitrum
```

**Webhook Trigger**: Sends `"detected"` webhook immediately when ERC-20 transfer found

### Backfill Worker (Historical)
**Purpose**: Fills gaps in historical data
**Frequency**: Every 30 seconds
**Range**: 5000 blocks window, auto-chunked into 1000-block RPC calls (Alchemy-safe)

```bash
# Run backfill worker
go run cmd/backfill/main.go --chain=42161  # Arbitrum
```

**Webhook Trigger**: Sends `"detected"` webhook when historical payment discovered

### Confirmation Worker (Finality)
**Purpose**: Sends final confirmation when transactions are final
**Frequency**: Every 30 seconds
**Process**: Checks scheduled events for required confirmations

```bash
# Run confirmation worker
go run cmd/confirmation/main.go --chain=42161  # Arbitrum
```

**Webhook Trigger**: Sends `"confirmed"` webhook when target confirmations reached

## 🔄 Complete Webhook Flow

```
1. Forward/Backfill Worker discovers ERC-20 transfer
   ↓
2. Sends "detected" webhook to backend immediately
   ↓
3. Schedules confirmation check in Redis
   ↓
4. Confirmation Worker monitors block progress
   ↓
5. When target confirmations reached → sends "confirmed" webhook
```

**Example Webhook Sequence**:
```json
// Webhook 1: Detected (immediate)
{
  "tx_hash": "0xabc...",
  "status": "detected",
  "confirmations": 5,
  "amount": "100.50",
  "token_symbol": "USDC"
}

// Webhook 2: Confirmed (when final)
{
  "tx_hash": "0xabc...",
  "status": "confirmed",
  "confirmations": 12,
  "amount": "100.50",
  "token_symbol": "USDC"
}
```

## 🔄 Transaction Flow

```
Blockchain → Workers → Redis → Backend
    ↓           ↓        ↓        ↓
  Source      Process  State   Final Storage
```

## 🛡️ Safety Features

- **Receipt Verification**: Only successful transactions get confirmed
- **Chain-Specific Finality**: Different confirmation requirements per blockchain
- **Deduplication**: Never process the same event twice
- **Overlap Prevention**: Forward/backfill workers coordinate properly

## 📊 Storage Summary

| Component | Purpose | Data Type | Duration |
|-----------|---------|-----------|----------|
| **Redis** | Fast state | Worker positions, deduplication | Hours-Days |
| **PostgreSQL** | Recovery | Worker cursors, stats | Permanent |
| **Backend** | Business data | Payment records | Permanent |
| **Blockchain** | Source | Raw transactions | Permanent |

## 🗄️ Database Schema Configuration

The indexer supports configurable database schemas for multi-environment deployment:

```bash
# .env configuration
DATABASE_URL=postgresql://user:pass@localhost:5432/appreal?sslmode=disable

# Schema Configuration
BACKEND_SCHEMA=public      # Local: public, Dev: appreal_dev, Prod: appreal_prod
INDEXER_SCHEMA=indexer     # Usually stays 'indexer' across all environments
```

### Schema Architecture

```
Same Database (appreal)
├── Backend Schema (BACKEND_SCHEMA)
│   ├── networks         # Chain configurations
│   ├── currencies       # Token contracts to watch
│   ├── wallets          # Merchant addresses to monitor
│   └── invoices         # Payment records
│
└── Indexer Schema (INDEXER_SCHEMA)
    ├── indexer_cursors          # Worker positions for recovery
    ├── indexer_processed_blocks # Audit trail of processed blocks
    └── indexer_stats            # Performance monitoring
```

### Running Migrations

```bash
# Option 1: Go migration tool (recommended)
# Uses INDEXER_SCHEMA from environment
go run cmd/migrate-indexer/migrate.go

# Option 2: Raw SQL with psql variables
psql -v indexer_schema=indexer \
  -f migrations/001_create_indexer_tables.sql \
  $DATABASE_URL

# For different schema name
psql -v indexer_schema=my_custom_schema \
  -f migrations/001_create_indexer_tables.sql \
  $DATABASE_URL
```

### Why Configurable Schemas?

1. **Multi-tenant deployments**: Different schemas per environment (dev, staging, prod)
2. **Legacy compatibility**: Backend may use non-standard schema names (e.g., `appreal_dev`)
3. **Clean separation**: Indexer tables isolated from backend tables
4. **Easy debugging**: Clear ownership of tables by schema

## 🎯 Key Features

- ✅ Multi-chain: Ethereum, Polygon, Arbitrum, BSC
- ✅ Efficient: Raw eth_getLogs with smart filtering
- ✅ Reliable: Error handling, retry logic, recovery
- ✅ Integrated: Seamless webhook integration
- ✅ Fast: Sub-millisecond Redis operations
- ✅ Scalable: Independent worker scaling

## 🔧 Quick Commands

```bash
# Start single worker
go run cmd/forward/main.go --chain=42161        # Arbitrum real-time
go run cmd/backfill/main.go --chain=42161       # Arbitrum historical
go run cmd/confirmation/main.go --chain=42161   # Arbitrum confirmations

# Monitor with Redis Insight
# Connect to: redis-master:6379 (host) or localhost:6381 (direct)
# Watch keys: idx:42161:cursor:forward, events:dedupe

# Environment variables (configure in .env)
FORWARD_INTERVAL=5s      # How often forward worker runs
BACKFILL_INTERVAL=30s    # How often backfill worker runs
BACKFILL_WINDOW_SIZE=5000 # Blocks per backfill batch
```

## 🎯 Confirmation Mechanism

### Chain-Specific Confirmations
- **Ethereum**: 12 confirmations (~3 minutes)
- **Arbitrum**: 10 confirmations (~10 seconds)
- **Polygon**: 20 confirmations (~1 minute)
- **BSC**: 15 confirmations (~45 seconds)

### Webhook Sequence
1. **"detected"**: Sent immediately when ERC-20 transfer found (5+ confirmations)
2. **"confirmed"**: Sent when target confirmations reached (final)

### Backend Integration
Your backend receives:
```bash
POST http://localhost:8001/api/v1/webhook/indexer
Content-Type: application/json
Authorization: ApiKey YOUR_API_KEY

{
  "tx_hash": "0xabc...",
  "status": "detected|confirmed",
  "confirmations": 12,
  "amount_formatted": "100.50",
  "token_symbol": "USDC",
  "to_address": "0x128..."
}
```

## 🎯 Perfect For

- **Payment processing systems** that need reliable transaction detection
- **DeFi applications** requiring multi-chain support
- **Wallet services** that monitor incoming transfers
- **Exchange platforms** that need final confirmation on deposits

The indexer processes payments and sends webhook events to your backend where you can handle the business logic! 🚀