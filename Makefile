.PHONY: help build run stop clean test docker-build docker-up docker-down dev dev-stop

# Default target
help:
	@echo "Available commands:"
	@echo ""
	@echo "BUILD & TEST:"
	@echo "  build              Build all binaries"
	@echo "  test               Run tests"
	@echo "  clean              Clean build artifacts"
	@echo ""
	@echo "LOCAL DEVELOPMENT:"
	@echo "  run                Run forward worker locally"
	@echo "  run-backfill       Run backfill worker locally"
	@echo "  dev                Start development environment"
	@echo "  dev-stop           Stop development environment"
	@echo ""
	@echo "MULTI-CHAIN DOCKER (Recommended):"
	@echo "  docker-multi-build Build multi-chain Docker image"
	@echo "  docker-multi-up    Start ALL chains (ETH, Polygon, Arbitrum, BSC)"
	@echo "  docker-multi-down  Stop all multi-chain workers"
	@echo "  docker-multi-logs  Show logs for all chains"
	@echo "  docker-multi-status Show status of all workers"
	@echo ""
	@echo "  docker-start-<chain>   Start specific chain (ethereum, polygon, arbitrum, bsc)"
	@echo "  docker-stop-<chain>    Stop specific chain"
	@echo "  docker-restart-<chain> Restart specific chain"
	@echo "  docker-logs-<chain>    Show logs for specific chain"
	@echo ""
	@echo "EXISTING INFRASTRUCTURE:"
	@echo "  existing           Start with existing infrastructure"
	@echo "  existing-stop      Stop workers running with existing infra"
	@echo "  health             Check existing infrastructure health"
	@echo "  logs               Show worker logs"
	@echo ""
	@echo "DATABASE:"
	@echo "  migrate-indexer    Run indexer schema migrations (Go tool)"
	@echo "  migrate-existing   Run migrations on existing infrastructure"
	@echo ""
	@echo "Examples:"
	@echo "  make docker-multi-build && make docker-multi-up  # Start all chains"
	@echo "  make docker-start-arbitrum                       # Start only Arbitrum"
	@echo "  make docker-logs-arbitrum                        # View Arbitrum logs"

# Build all binaries
build:
	@echo "Building binaries..."
	go build -o bin/forward ./cmd/forward
	go build -o bin/backfill ./cmd/backfill
	go build -o bin/confirmation ./cmd/confirmation
	@echo "Build complete!"

# Run forward worker locally
run:
	@echo "Running forward worker..."
	go run ./cmd/forward ${CHAIN_ID:-1}

# Run backfill worker locally
run-backfill:
	@echo "Running backfill worker..."
	go run ./cmd/backfill ${CHAIN_ID:-1}

# Run confirmation worker locally
run-confirmation:
	@echo "Running confirmation worker..."
	go run ./cmd/confirmation ${CHAIN_ID:-1}

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Build Docker images
docker-build:
	@echo "Building Docker images..."
	docker build -t indexer-go-v2:latest .
	docker build -f Dockerfile.dev -t indexer-go-v2:dev .

# Start all services
docker-up:
	@echo "Starting services..."
	docker-compose up -d

# Stop all services
docker-down:
	@echo "Stopping services..."
	docker-compose down

# Start development environment
dev:
	@echo "Starting development environment..."
	docker-compose -f docker-compose.dev.yml up -d postgres redis
	@echo "Starting forward worker with hot reload..."
	docker-compose -f docker-compose.dev.yml up forward-ethereum-dev

# Stop development environment
dev-stop:
	@echo "Stopping development environment..."
	docker-compose -f docker-compose.dev.yml down

# Watch services logs
logs:
	docker-compose logs -f

# Watch development logs
dev-logs:
	docker-compose -f docker-compose.dev.yml logs -f

# Clean build artifacts and containers
clean:
	@echo "Cleaning up..."
	rm -rf bin/
	rm -rf tmp/
	rm -f coverage.out coverage.html
	docker-compose down -v
	docker-compose -f docker-compose.dev.yml down -v
	docker system prune -f

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Lint code
lint:
	@echo "Linting code..."
	golangci-lint run

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

# Generate documentation
docs:
	@echo "Generating documentation..."
	godoc -http=:6060

# Database migrations
migrate-up:
	@echo "Running database migrations..."
	migrate -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	@echo "Rolling back database migrations..."
	migrate -path migrations -database "$(DATABASE_URL)" down

# Install development tools
install-tools:
	@echo "Installing development tools..."
	go install github.com/cosmtrek/air@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# =============================================================================
# EXISTING INFRASTRUCTURE COMMANDS
# =============================================================================

# Start with existing infrastructure
existing:
	@echo "Starting indexer with existing infrastructure..."
	./scripts/start.sh start

# Stop workers with existing infrastructure
existing-stop:
	@echo "Stopping indexer workers..."
	./scripts/start.sh stop

# Check infrastructure health
health:
	@echo "Checking existing infrastructure health..."
	./scripts/start.sh health

# Show worker logs
logs:
	@echo "Showing worker logs..."
	./scripts/start.sh logs

# Run database migrations with existing infra
migrate-existing:
	@echo "Running database migrations on existing infrastructure..."
	./scripts/start.sh migrate

# Start with Docker and existing infrastructure
docker-existing:
	@echo "Starting indexer containers with existing infrastructure..."
	docker-compose -f docker-compose.existing.yml up -d

# Stop Docker containers with existing infrastructure
docker-existing-down:
	@echo "Stopping indexer containers with existing infrastructure..."
	docker-compose -f docker-compose.existing.yml down

# Show Docker logs for existing infrastructure
docker-existing-logs:
	@echo "Showing Docker logs for existing infrastructure..."
	docker-compose -f docker-compose.existing.yml logs -f

# Quick start (assumes existing infrastructure is running)
quick-start:
	@echo "Quick starting indexer (assumes existing infrastructure)..."
	@echo "Starting Ethereum and Polygon workers..."
	CHAIN_ID=1 go run ./cmd/forward/main.go &
	CHAIN_ID=137 go run ./cmd/forward/main.go &
	CHAIN_ID=1 go run ./cmd/backfill/main.go &
	CHAIN_ID=137 go run ./cmd/backfill/main.go &
	CHAIN_ID=1 go run ./cmd/confirmation/main.go &
	CHAIN_ID=137 go run ./cmd/confirmation/main.go &
	@echo "Workers started in background. Use 'make logs' or 'make status' to monitor."

# =============================================================================
# MULTI-CHAIN DOCKER COMMANDS
# =============================================================================

# Build multi-chain Docker image
docker-multi-build:
	@echo "Building multi-chain indexer image..."
	docker build -t indexer-go-v2:multi-chain .

# Start all chains (Ethereum, Polygon, Arbitrum, BSC)
docker-multi-up:
	@echo "Starting forward and backfill workers for all chains..."
	@echo "Chains: Ethereum (1), Polygon (137), Arbitrum (42161), BSC (56)"
	docker-compose -f docker-compose.multi-chain.yml up -d
	@echo ""
	@echo "Workers started! Use 'make docker-multi-logs' to monitor."

# Stop all multi-chain workers
docker-multi-down:
	@echo "Stopping all multi-chain workers..."
	docker-compose -f docker-compose.multi-chain.yml down

# Show logs for all chains
docker-multi-logs:
	docker-compose -f docker-compose.multi-chain.yml logs -f

# Show status of all workers
docker-multi-status:
	@echo "Multi-chain worker status:"
	@echo "=========================="
	docker-compose -f docker-compose.multi-chain.yml ps

# Restart specific chain workers
docker-restart-ethereum:
	@echo "Restarting Ethereum workers..."
	docker-compose -f docker-compose.multi-chain.yml restart forward-ethereum backfill-ethereum

docker-restart-polygon:
	@echo "Restarting Polygon workers..."
	docker-compose -f docker-compose.multi-chain.yml restart forward-polygon backfill-polygon

docker-restart-arbitrum:
	@echo "Restarting Arbitrum workers..."
	docker-compose -f docker-compose.multi-chain.yml restart forward-arbitrum backfill-arbitrum

docker-restart-bsc:
	@echo "Restarting BSC workers..."
	docker-compose -f docker-compose.multi-chain.yml restart forward-bsc backfill-bsc

# Start specific chain only
docker-start-ethereum:
	@echo "Starting Ethereum workers only..."
	docker-compose -f docker-compose.multi-chain.yml up -d forward-ethereum backfill-ethereum

docker-start-polygon:
	@echo "Starting Polygon workers only..."
	docker-compose -f docker-compose.multi-chain.yml up -d forward-polygon backfill-polygon

docker-start-arbitrum:
	@echo "Starting Arbitrum workers only..."
	docker-compose -f docker-compose.multi-chain.yml up -d forward-arbitrum backfill-arbitrum

docker-start-bsc:
	@echo "Starting BSC workers only..."
	docker-compose -f docker-compose.multi-chain.yml up -d forward-bsc backfill-bsc

# Stop specific chain only
docker-stop-ethereum:
	@echo "Stopping Ethereum workers..."
	docker-compose -f docker-compose.multi-chain.yml stop forward-ethereum backfill-ethereum

docker-stop-polygon:
	@echo "Stopping Polygon workers..."
	docker-compose -f docker-compose.multi-chain.yml stop forward-polygon backfill-polygon

docker-stop-arbitrum:
	@echo "Stopping Arbitrum workers..."
	docker-compose -f docker-compose.multi-chain.yml stop forward-arbitrum backfill-arbitrum

docker-stop-bsc:
	@echo "Stopping BSC workers..."
	docker-compose -f docker-compose.multi-chain.yml stop forward-bsc backfill-bsc

# Logs for specific chain
docker-logs-ethereum:
	docker-compose -f docker-compose.multi-chain.yml logs -f forward-ethereum backfill-ethereum

docker-logs-polygon:
	docker-compose -f docker-compose.multi-chain.yml logs -f forward-polygon backfill-polygon

docker-logs-arbitrum:
	docker-compose -f docker-compose.multi-chain.yml logs -f forward-arbitrum backfill-arbitrum

docker-logs-bsc:
	docker-compose -f docker-compose.multi-chain.yml logs -f forward-bsc backfill-bsc

# Run migrations for indexer schema
migrate-indexer:
	@echo "Running indexer schema migrations..."
	go run cmd/migrate-indexer/migrate.go
	@echo "Migration complete!"