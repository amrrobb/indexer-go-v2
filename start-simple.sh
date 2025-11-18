#!/bin/bash
# Simple starter without psql/redis-cli dependency checks

echo "Starting indexer workers..."
echo "Environment: $(grep DATABASE_URL .env | cut -d'=' -f1)"

# Start workers in background
go run cmd/forward/main.go &
FORWARD_PID=$!

go run cmd/backfill/main.go &
BACKFILL_PID=$!

go run cmd/confirmation/main.go &
CONFIRMATION_PID=$!

echo ""
echo "✅ Workers started:"
echo "   Forward:     PID $FORWARD_PID"
echo "   Backfill:    PID $BACKFILL_PID"
echo "   Confirmation: PID $CONFIRMATION_PID"
echo ""
echo "Stop with: kill $FORWARD_PID $BACKFILL_PID $CONFIRMATION_PID"
echo "Or: pkill -f 'main.go'"