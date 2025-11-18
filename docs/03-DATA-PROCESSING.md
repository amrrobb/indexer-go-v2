# Data Processing & Storage

## 🎯 What Gets Stored Where

### Redis (Fast, Temporary)
```
Worker Positions: idx:{chain}:cursor:{worker}
- Forward: last block processed
- Backfill: last block processed
- Recovery: worker restart positions

Event Deduplication: events:dedupe (SET)
- All processed event IDs
- Prevents duplicate processing

Confirmation Scheduling: events:confirm:{eventId}
- Target block for confirmation
- Confirmation timing

Event Details: events:details:{eventId}
- Temporary storage for confirmation processing
- Expires after 24 hours
```

### PostgreSQL (Minimal, Recovery)
```sql
-- ONLY writes worker cursors for recovery
CREATE TABLE indexer_cursors (
    id SERIAL PRIMARY KEY,
    chain_id INTEGER NOT NULL,
    worker_type VARCHAR(20) NOT NULL, -- 'forward', 'backfill', 'confirmation'
    last_block BIGINT NOT NULL,
    updated_at TIMESTAMP DEFAULT NOW()
);
```

### Backend (Final Storage - YOURS)
```
The indexer sends webhooks, backend stores:
- Payment records
- Transaction history
- User balances
- Business data
```

## 📊 Data Flow

```
Blockchain → Workers (memory) → Redis (state) → Backend (final storage)
    ↓              ↓                ↓               ↓
  Source         Process        Coordination      Business Records
```

## 🧹 Cleanup Strategy

- **Redis**: Automatic TTL expiration
- **PostgreSQL**: Minimal data, periodic cleanup
- **Backend**: Your retention policy

## 🎯 Key Point

**Indexer = Processing Engine**
- Reads from blockchain
- Processes in memory
- Uses Redis for coordination
- Sends to backend
- Stores minimal recovery data