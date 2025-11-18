# Transaction Confirmation Mechanism

## 🎯 Core Question: "How do we know a transaction is confirmed?"

**Detection ≠ Confirmation**: Always wait for final verification

## 🔄 Confirmation Process

### 1. Initial Detection (5 confirmations)
```
Transaction detected → "detected" webhook → Backend: Payment = PENDING
```

### 2. Waiting for Finality
```
Chain-specific confirmations needed:
- Ethereum: 12 blocks (~3 minutes)
- Polygon: 20 blocks (~1 minute)
- Arbitrum: 10 blocks (~10 seconds)
- BSC: 15 blocks (~45 seconds)
```

### 3. Final Verification
```
Current block ≥ target block?
├── YES → Get transaction receipt
│   └── receipt.status == 1?
│       ├── YES → "confirmed" webhook
│       └── NO → Remove from schedule
└── NO → Wait for next check
```

## 🛡️ Safety Mechanisms

- **Receipt Verification**: Only successful transactions confirmed
- **Chain-Specific Finality**: Different confirmation requirements
- **Failure Handling**: Failed transactions rejected
- **Reorg Protection**: Wait for blockchain finality

## 📊 Real Example

```
Ethereum USDC Transfer:
1. Detected at block 19,000,000 (5 confirmations)
2. Wait until block 19,000,012 (12 confirmations)
3. Verify transaction receipt.status == 1
4. Send "confirmed" webhook
5. Backend: Payment = CONFIRMED
```