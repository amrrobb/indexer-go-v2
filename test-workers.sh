#!/bin/bash

echo "=== 🧪 Indexer Go v2 - Worker Compilation Test ==="
echo ""

# Test 1: Compile all workers
echo "📦 Testing worker compilation..."

echo "   Forward worker:"
if go build cmd/forward/main.go; then
    echo "   ✅ Forward worker compiles successfully"
else
    echo "   ❌ Forward worker compilation failed"
    exit 1
fi

echo "   Backfill worker:"
if go build cmd/backfill/main.go; then
    echo "   ✅ Backfill worker compiles successfully"
else
    echo "   ❌ Backfill worker compilation failed"
    exit 1
fi

echo "   Confirmation worker:"
if go build cmd/confirmation/main.go; then
    echo "   ✅ Confirmation worker compiles successfully"
else
    echo "   ❌ Confirmation worker compilation failed"
    exit 1
fi

echo ""
echo "✅ All workers compile successfully!"
echo ""

# Test 2: Show available run commands
echo "🚀 Available worker commands:"
echo ""
echo "   Forward Worker (Real-time):"
echo "      go run cmd/forward/main.go --chain=42161"
echo ""
echo "   Backfill Worker (Historical):"
echo "      go run cmd/backfill/main.go --chain=42161"
echo ""
echo "   Confirmation Worker (Finality):"
echo "      go run cmd/confirmation/main.go --chain=42161"
echo ""

echo "📊 Configuration for chain 42161 (Arbitrum):"
echo "   - 21 watched addresses"
echo "   - 3 active currencies (USDC, USDT, USDTa)"
echo "   - Redis Sentinel with automatic fallback"
echo "   - Enhanced logging enabled"
echo ""

echo "🎯 Ready to start indexing!"
echo "   Note: ERPC endpoint still needs public access for full functionality"