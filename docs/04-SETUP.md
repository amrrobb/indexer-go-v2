# Setup with Existing Infrastructure

## 🚀 Quick Start

### Prerequisites
1. **PostgreSQL**: `localhost:15432` (from appreal-infra)
2. **Redis**: `localhost:6379` (from appreal-infra redis-standalone)
3. **ERPC**: `localhost:4000` (from appreal-erpc)
4. **Backend**: `localhost:8000` (your backend API)

### Start Commands

```bash
# Check infrastructure health
make health

# Run database migrations (first time)
make migrate-existing

# Start workers (Ethereum + Polygon)
make existing

# Monitor logs
make logs
```

## 🔧 Configuration

The indexer is pre-configured for your existing infrastructure:

```bash
# Database (appreal-infra)
DATABASE_URL=postgresql://postgres:password@localhost:15432/postgres?sslmode=disable

# Redis (appreal-infra)
REDIS_URL=localhost:6379
REDIS_PASSWORD=admin

# RPC (appreal-erpc)
ETHEREUM_RPC_URL=http://localhost:4000/ethereum/1
POLYGON_RPC_URL=http://localhost:4000/polygon/137

# Backend
WEBHOOK_URL=http://localhost:8000/webhooks/payments
```

## 🐳 Docker Option

```bash
# Start with Docker + existing infrastructure
make docker-existing
```

## 🔍 Troubleshooting

```bash
# Check database connection
psql postgresql://postgres:password@localhost:15432/postgres -c "SELECT 1;"

# Check Redis connection
redis-cli -h localhost -p 6379 -a admin ping

# Check ERPC connection
curl http://localhost:4000/health
```