# Architecture Overview

## 🎯 What This Is

**indexer-go-v2** is a lightweight, high-performance payment processing engine that:
- Detects ERC20 token transfers to watched addresses
- Processes them through a complete lifecycle (detected → confirmed)
- Integrates seamlessly with your existing backend infrastructure

## 🏗️ System Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              INDEXER GO V2                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                           │
│  │ Forward     │  │ Backfill    │  │Confirmation │                           │
│  │ Worker      │  │ Worker      │  │ Worker      │                           │
│  │             │  │             │  │             │                           │
│  │ Real-time   │  │ Historical  │  │ Finality    │                           │
│  │ Detection   │  │ Gap Filling │  │ Checking    │                           │
│  └─────────────┘  └─────────────┘  └─────────────┘                           │
│         │                │                │                                  │
│         ▼                ▼                ▼                                  │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                        SHARED INFRASTRUCTURE                          │   │
│  ├─────────────────────────────────────────────────────────────────────┤   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐ │   │
│  │  │   Redis     │  │ PostgreSQL  │  │   ERPC      │  │   Webhook   │ │   │
│  │  │ (State)     │  │(Recovery)   │  │ (Blockchain)│  │  (Backend)  │ │   │
│  │  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘ │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                     │                                      │
│                                     ▼                                      │
├─────────────────────────────────────────────────────────────────────────────┤
│                              EXISTING INFRASTRUCTURE                          │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────────────┐  │
│  │  Backend API    │  │   PostgreSQL    │  │         Redis               │  │
│  │                 │  │                 │  │                             │  │
│  │  Webhook Receiver│  │  Chains/Currencies│  │     Existing State         │  │
│  │  Payment API    │  │  Wallets Config  │  │                             │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 🔄 Data Flow

1. **Blockchain** → Workers detect ERC20 transfers
2. **Workers** → Process in memory, use Redis for state
3. **Redis** → Temporary storage for coordination
4. **Webhook** → Send payment data to backend
5. **Backend** → Store final payment records

## 🧩 Three-Worker System

### Forward Worker (Real-time)
- Processes blocks: `current-15` to `current-5` (10 blocks)
- Runs every 3 seconds
- Sends "detected" webhook immediately
- Schedules confirmation check

### Backfill Worker (Historical)
- Processes large historical ranges (1k-10k blocks)
- Runs every 30 seconds
- Skips blocks processed by forward worker
- Fills gaps in payment history

### Confirmation Worker (Finality)
- Processes scheduled confirmations every 30 seconds
- Verifies transaction receipts
- Sends "confirmed" webhook when finalized
- Handles failed transactions

## 🎯 Key Features

- ✅ **Multi-chain**: Ethereum, Polygon, Arbitrum, BSC
- ✅ **No Genesis Sync**: Starts from current block
- ✅ **Efficient**: Raw eth_getLogs with smart filtering
- ✅ **Reliable**: Duplicate prevention, error handling
- ✅ **Integrated**: Seamless backend webhook integration
- ✅ **Fast**: Sub-millisecond Redis operations
- ✅ **Recovery**: PostgreSQL backup of worker positions