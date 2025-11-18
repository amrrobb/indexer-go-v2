#!/bin/bash

# =============================================================================
# Indexer Go v2 - Startup Script for Existing Infrastructure
# =============================================================================

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# Function to print colored output
print_status() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_header() {
    echo -e "${BLUE}=============================================================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}=============================================================================${NC}"
}

# Function to check if service is running
check_service() {
    local service_name=$1
    local check_command=$2

    if eval "$check_command" >/dev/null 2>&1; then
        print_status "$service_name is running ✓"
        return 0
    else
        print_warning "$service_name is not running ✗"
        return 1
    fi
}

# Function to check database connection
check_database() {
    print_status "Checking database connection..."

    if psql "$DATABASE_URL" -c "SELECT 1;" >/dev/null 2>&1; then
        print_status "Database connection successful ✓"
        return 0
    else
        print_error "Cannot connect to database"
        print_error "Please ensure PostgreSQL is running on port 15432"
        print_error "Run: cd ../appreal-infra && docker-compose up -d postgres"
        return 1
    fi
}

# Function to check Redis connection
check_redis() {
    print_status "Checking Redis connection..."

    if redis-cli -h localhost -p 6379 -a "$REDIS_PASSWORD" ping >/dev/null 2>&1; then
        print_status "Redis connection successful ✓"
        return 0
    else
        print_error "Cannot connect to Redis"
        print_error "Please ensure Redis is running on port 6379"
        print_error "Run: cd ../appreal-infra && docker-compose -f redis-standalone.docker-compose.yml up -d"
        return 1
    fi
}

# Function to run database migrations
run_migrations() {
    print_status "Running database migrations..."

    cd "$PROJECT_DIR"
    if go run ./cmd/migrate/main.go up; then
        print_status "Migrations completed successfully ✓"
    else
        print_error "Migration failed"
        return 1
    fi
}

# Function to start workers
start_workers() {
    local chains=("$@")

    print_status "Starting indexer workers..."

    cd "$PROJECT_DIR"

    # Create logs directory
    mkdir -p logs

    # Start workers in background
    for chain in "${chains[@]}"; do
        local chain_name=""
        case $chain in
            1) chain_name="ethereum" ;;
            137) chain_name="polygon" ;;
            42161) chain_name="arbitrum" ;;
            56) chain_name="bsc" ;;
            *) chain_name="chain-$chain" ;;
        esac

        print_status "Starting forward worker for $chain_name..."
        nohup go run ./cmd/forward/main.go "$chain" > "logs/forward-$chain_name.log" 2>&1 &

        print_status "Starting backfill worker for $chain_name..."
        nohup go run ./cmd/backfill/main.go "$chain" > "logs/backfill-$chain_name.log" 2>&1 &

        print_status "Starting confirmation worker for $chain_name..."
        nohup go run ./cmd/confirmation/main.go "$chain" > "logs/confirmation-$chain_name.log" 2>&1 &

        sleep 2  # Give workers time to start
    done

    print_status "All workers started ✓"
}

# Function to check worker health
check_workers() {
    print_status "Checking worker health..."

    # Check if processes are running
    if pgrep -f "cmd/forward/main.go" >/dev/null; then
        print_status "Forward workers running ✓"
    else
        print_warning "No forward workers found"
    fi

    if pgrep -f "cmd/backfill/main.go" >/dev/null; then
        print_status "Backfill workers running ✓"
    else
        print_warning "No backfill workers found"
    fi

    if pgrep -f "cmd/confirmation/main.go" >/dev/null; then
        print_status "Confirmation workers running ✓"
    else
        print_warning "No confirmation workers found"
    fi
}

# Function to stop workers
stop_workers() {
    print_status "Stopping all indexer workers..."

    pkill -f "cmd/forward/main.go" || true
    pkill -f "cmd/backfill/main.go" || true
    pkill -f "cmd/confirmation/main.go" || true

    sleep 3

    print_status "Workers stopped ✓"
}

# Function to show logs
show_logs() {
    local chain=${1:-""}

    cd "$PROJECT_DIR/logs"

    if [ -z "$chain" ]; then
        print_status "Showing all logs (Press Ctrl+C to exit)..."
        tail -f *.log
    else
        print_status "Showing logs for chain: $chain"
        tail -f "forward-$chain.log" "backfill-$chain.log" "confirmation-$chain.log" 2>/dev/null || print_warning "No logs found for chain: $chain"
    fi
}

# Function to check dependencies
check_dependencies() {
    print_status "Checking dependencies..."

    # Check Go
    if command -v go >/dev/null 2>&1; then
        GO_VERSION=$(go version | awk '{print $3}')
        print_status "Go installed: $GO_VERSION ✓"
    else
        print_error "Go is not installed"
        return 1
    fi

    # Check PostgreSQL client
    if command -v psql >/dev/null 2>&1; then
        print_status "PostgreSQL client installed ✓"
    else
        print_error "PostgreSQL client is not installed"
        return 1
    fi

    # Check Redis client
    if command -v redis-cli >/dev/null 2>&1; then
        print_status "Redis client installed ✓"
    else
        print_error "Redis client is not installed"
        return 1
    fi
}

# Function to show usage
show_usage() {
    echo "Usage: $0 [COMMAND] [OPTIONS]"
    echo ""
    echo "Commands:"
    echo "  start [chains...]    Start indexer workers (default: ethereum polygon)"
    echo "  stop                 Stop all indexer workers"
    echo "  restart [chains...]  Restart indexer workers"
    echo "  status               Check worker status"
    echo "  logs [chain]         Show logs (chain optional)"
    echo "  health               Check infrastructure health"
    echo "  migrate              Run database migrations"
    echo ""
    echo "Examples:"
    echo "  $0 start                     # Start ethereum and polygon"
    echo "  $0 start 1 137                # Start by chain ID"
    echo "  $0 start ethereum polygon    # Start by chain name"
    echo "  $0 logs ethereum             # Show ethereum logs"
    echo "  $0 status                    # Check worker status"
    echo ""
    echo "Chains:"
    echo "  ethereum, 1        Ethereum Mainnet"
    echo "  polygon, 137       Polygon Mainnet"
    echo "  arbitrum, 42161    Arbitrum One"
    echo "  bsc, 56           BSC Mainnet"
}

# Main execution
main() {
    print_header "Indexer Go v2 - Startup Script"

    # Source environment variables
    if [ -f "$PROJECT_DIR/.env" ]; then
        source "$PROJECT_DIR/.env"
        print_status "Environment variables loaded from .env"
    else
        print_error ".env file not found. Please create it from .env.example"
        exit 1
    fi

    case "${1:-help}" in
        "health")
            check_dependencies
            check_database
            check_redis
            check_workers
            ;;
        "migrate")
            check_database
            run_migrations
            ;;
        "start")
            # Default chains if none provided
            local chains=("${@:2}")
            if [ ${#chains[@]} -eq 0 ]; then
                chains=("ethereum" "polygon")
            fi

            check_dependencies
            check_database
            check_redis
            start_workers "${chains[@]}"
            check_workers
            print_status ""
            print_status "Indexer started successfully!"
            print_status "Use '$0 logs' to monitor logs"
            print_status "Use '$0 status' to check worker status"
            print_status "Use '$0 stop' to stop all workers"
            ;;
        "stop")
            stop_workers
            ;;
        "restart")
            stop_workers
            sleep 2
            main start "${@:2}"
            ;;
        "status")
            check_workers
            ;;
        "logs")
            show_logs "${2:-}"
            ;;
        "help"|"--help"|"-h")
            show_usage
            ;;
        *)
            print_error "Unknown command: $1"
            show_usage
            exit 1
            ;;
    esac
}

# Execute main function with all arguments
main "$@"