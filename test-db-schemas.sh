#!/bin/bash

echo "=== 🗄️ Database Schema Connection Test ==="
echo ""

# Load environment variables
set -a && source .env && set +a

echo "🔗 Testing database connection..."
echo "   DATABASE_URL: ${DATABASE_URL:0:50}..."
echo ""

# Create a simple Go program to test both schema connections
cat > test_schemas.go << 'EOF'
package main

import (
    "context"
    "fmt"
    "log"

    "indexer-go-v2/internal/database"
)

func main() {
    fmt.Println("🗄️ Testing both database schema connections...")

    // Initialize database client (creates both pools)
    db, err := database.NewClient()
    if err != nil {
        log.Fatalf("❌ Failed to initialize database: %v", err)
    }
    defer db.Close()

    ctx := context.Background()

    // Test ConfigPool (public schema)
    fmt.Println("📋 Testing ConfigPool (public schema)...")
    err = db.ConfigPool.Ping(ctx)
    if err != nil {
        log.Fatalf("❌ ConfigPool connection failed: %v", err)
    }
    fmt.Println("   ✅ ConfigPool (public schema) connected successfully")

    // Test IndexerPool (indexer schema)
    fmt.Println("📊 Testing IndexerPool (indexer schema)...")
    err = db.IndexerPool.Ping(ctx)
    if err != nil {
        log.Fatalf("❌ IndexerPool connection failed: %v", err)
    }
    fmt.Println("   ✅ IndexerPool (indexer schema) connected successfully")

    // Test health check (uses both pools)
    fmt.Println("💊 Testing overall health check...")
    err = db.Health()
    if err != nil {
        log.Fatalf("❌ Health check failed: %v", err)
    }
    fmt.Println("   ✅ Overall health check passed")

    fmt.Println("")
    fmt.Println("🎯 Both schema pools are working correctly!")
    fmt.Println("   - ConfigPool: Reading backend configuration (public schema)")
    fmt.Println("   - IndexerPool: Indexer operations (indexer schema)")
    fmt.Println("   - Both pools: Connection pooling, health checks, and separate resource limits")
}
EOF

# Run the test
echo "🧪 Running schema connection test..."
go run test_schemas.go

# Clean up
rm test_schemas.go

echo ""
echo "✅ Database schema test completed!"