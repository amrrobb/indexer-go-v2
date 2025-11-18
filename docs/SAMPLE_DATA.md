# Sample Data for Indexer Operations

This document shows example data that will be stored in the indexer schema tables.

## Tables Overview

The indexer creates 3 tables in the `indexer` schema:

1. **`indexer_cursors`** - Tracks worker positions
2. **`indexer_processed_blocks`** - Prevents duplicate processing
3. **`indexer_stats`** - Optional monitoring statistics

---

## 1. indexer_cursors Sample Data

| chain_id | worker_type | last_block | updated_at |
|----------|-------------|------------|------------|
| 1        | forward     | 18500000   | 2025-01-15 10:30:00 |
| 1        | backfill    | 18450000   | 2025-01-15 10:29:00 |
| 1        | confirmation| 18499900   | 2025-01-15 10:30:00 |
| 137      | forward     | 52000000   | 2025-01-15 10:30:00 |
| 137      | backfill    | 51950000   | 2025-01-15 10:29:00 |
| 137      | confirmation| 51999900   | 2025-01-15 10:30:00 |

**Explanation:**
- **forward**: Processes new blocks as they are mined (real-time)
- **backfill**: Processes historical blocks to fill gaps
- **confirmation**: Tracks blocks that need confirmation checking

---

## 2. indexer_processed_blocks Sample Data

| id | chain_id | block_number | worker_type | processed_at |
|----|----------|--------------|-------------|--------------|
| 1  | 1        | 18500000     | forward     | 2025-01-15 10:30:00 |
| 2  | 1        | 18500001     | forward     | 2025-01-15 10:30:03 |
| 3  | 1        | 18450000     | backfill    | 2025-01-15 10:29:00 |
| 4  | 137      | 52000000     | forward     | 2025-01-15 10:30:00 |
| 5  | 137      | 52000001     | forward     | 2025-01-15 10:30:03 |
| 6  | 137      | 51950000     | backfill    | 2025-01-15 10:29:00 |

**Overlap Prevention:**
- Each block+worker_type combination is unique
- Forward and backfill workers can process the same block independently
- Prevents the same worker from processing a block twice

---

## 3. indexer_stats Sample Data

| id | chain_id | worker_type | blocks_processed | events_detected | events_confirmed | last_activity |
|----|----------|-------------|------------------|-----------------|------------------|---------------|
| 1  | 1        | forward     | 1200             | 45              | 42               | 2025-01-15 10:30:00 |
| 2  | 1        | backfill    | 5000             | 180             | 175              | 2025-01-15 10:29:00 |
| 3  | 1        | confirmation| 1150             | 0               | 42               | 2025-01-15 10:30:00 |
| 4  | 137      | forward     | 800              | 25              | 24               | 2025-01-15 10:30:00 |
| 5  | 137      | backfill    | 3000             | 95              | 90               | 2025-01-15 10:29:00 |

**Performance Monitoring:**
- `blocks_processed`: Total blocks this worker has processed
- `events_detected`: ERC20 transfer events found
- `events_confirmed`: Events that reached required confirmations

---

## 4. Example Processing Flow

### Forward Worker (Ethereum)
```
Current block: 18500001
- Processes block 18500001
- Stores: indexer_cursors(1, forward, 18500001)
- Stores: indexer_processed_blocks(1, 18500001, forward)
- Finds: USDC transfer to 0x1234... (detected event)
- Sends webhook: {"status": "detected", "tx_hash": "0xabcd...", ...}
```

### Backfill Worker (Ethereum)
```
Gap found: 18450000 - 18449900
- Processes block 18450000
- Stores: indexer_cursors(1, backfill, 18450000)
- Stores: indexer_processed_blocks(1, 18450000, backfill)
- Finds: USDT transfer to 0x5678... (detected event)
- Sends webhook: {"status": "detected", "tx_hash": "0x1234...", ...}
```

### Confirmation Worker (Ethereum)
```
Checking block 18499900 (12 confirmations needed)
- Block 18499900 has 15 confirmations ✓
- Processes pending events from that block
- Updates: indexer_cursors(1, confirmation, 18499900)
- Sends webhook: {"status": "confirmed", "tx_hash": "0xabcd...", ...}
```

---

## 5. Worker Startup Simulation

When workers start, they will:

1. **Load configuration** from backend (public schema):
   ```sql
   SELECT * FROM networks WHERE blockchain_network_type = 'EVM' AND is_active = true;
   SELECT * FROM currencies WHERE network_id = 'ethereum' AND is_used_for_payment = true;
   SELECT * FROM wallets WHERE network_id = 'ethereum' AND type = 'DEPOSIT';
   ```

2. **Initialize cursors** (first time only):
   ```sql
   INSERT INTO indexer.indexer_cursors (chain_id, worker_type, last_block)
   VALUES (1, 'forward', 18500000), (137, 'forward', 52000000);
   ```

3. **Start processing**:
   - Forward worker: Current block + 1
   - Backfill worker: Genesis block or configured start
   - Confirmation worker: Check for pending events

---

## 6. Database Schema Separation

**Backend reads (ConfigPool):**
- ✅ `public.networks` - Chain configuration
- ✅ `public.currencies` - Token information
- ✅ `public.wallets` - Deposit addresses

**Indexer writes (IndexerPool):**
- ✅ `indexer.indexer_cursors` - Worker positions
- ✅ `indexer.indexer_processed_blocks` - Overlap prevention
- ✅ `indexer.indexer_stats` - Performance metrics

This separation prevents resource competition between backend and indexer operations.