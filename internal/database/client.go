package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // for sql.DB compatibility
	"github.com/sirupsen/logrus"
)

// Client represents database connection with two pools
type Client struct {
	ConfigPool    *pgxpool.Pool // For reading backend config (public schema)
	IndexerPool   *pgxpool.Pool // For indexer operations (indexer schema)
	DB            *sql.DB       // For compatibility with existing code
	BackendSchema string        // Schema name for backend tables (e.g., "public", "appreal_dev")
	IndexerSchema string        // Schema name for indexer tables (e.g., "indexer")
}

// Config holds database configuration
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
	SSLMode  string
}

// NewClient creates a new database client with two separate pools
func NewClient() (*Client, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is required")
	}

	// Get schema configuration from environment
	backendSchema := os.Getenv("BACKEND_SCHEMA")
	if backendSchema == "" {
		backendSchema = "public" // Default to public schema
	}

	indexerSchema := os.Getenv("INDEXER_SCHEMA")
	if indexerSchema == "" {
		indexerSchema = "indexer" // Default to indexer schema
	}

	// Get connection pool configuration from environment
	configMaxConns := getIntEnv("CONFIG_POOL_MAX_CONNS", 5)
	configMinConns := getIntEnv("CONFIG_POOL_MIN_CONNS", 2)
	indexerMaxConns := getIntEnv("INDEXER_POOL_MAX_CONNS", 10)
	indexerMinConns := getIntEnv("INDEXER_POOL_MIN_CONNS", 3)

	// Create ConfigPool for backend reads (public schema)
	configPoolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config pool config: %w", err)
	}
	configPoolConfig.MaxConns = int32(configMaxConns)
	configPoolConfig.MinConns = int32(configMinConns)

	configPool, err := pgxpool.NewWithConfig(context.Background(), configPoolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create config connection pool: %w", err)
	}

	// Create IndexerPool for indexer operations (indexer schema)
	indexerPoolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse indexer pool config: %w", err)
	}
	indexerPoolConfig.MaxConns = int32(indexerMaxConns)
	indexerPoolConfig.MinConns = int32(indexerMinConns)

	indexerPool, err := pgxpool.NewWithConfig(context.Background(), indexerPoolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create indexer connection pool: %w", err)
	}

	// Test both connections
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := configPool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping config database: %w", err)
	}

	if err := indexerPool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping indexer database: %w", err)
	}

	// Create sql.DB for compatibility
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open sql.DB: %w", err)
	}

	// Test sql.DB connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping sql.DB: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"config_max_conns":  configMaxConns,
		"config_min_conns":  configMinConns,
		"indexer_max_conns": indexerMaxConns,
		"indexer_min_conns": indexerMinConns,
		"backend_schema":    backendSchema,
		"indexer_schema":    indexerSchema,
	}).Info("Database connection established successfully with two pools")

	return &Client{
		ConfigPool:    configPool,
		IndexerPool:   indexerPool,
		DB:            db,
		BackendSchema: backendSchema,
		IndexerSchema: indexerSchema,
	}, nil
}

// getIntEnv gets integer environment variable with default value
func getIntEnv(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
		logrus.WithField("key", key).Warn("Invalid integer environment value, using default")
	}
	return defaultValue
}

// Close closes the database connections
func (c *Client) Close() error {
	if c.ConfigPool != nil {
		c.ConfigPool.Close()
	}
	if c.IndexerPool != nil {
		c.IndexerPool.Close()
	}
	if c.DB != nil {
		if err := c.DB.Close(); err != nil {
			logrus.WithError(err).Error("Error closing sql.DB")
			return err
		}
	}
	return nil
}

// Health checks database health for both pools
func (c *Client) Health() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check ConfigPool
	if err := c.ConfigPool.Ping(ctx); err != nil {
		return fmt.Errorf("config pool health check failed: %w", err)
	}

	// Check IndexerPool
	if err := c.IndexerPool.Ping(ctx); err != nil {
		return fmt.Errorf("indexer pool health check failed: %w", err)
	}

	return nil
}

// RunMigration runs the provided SQL migration on indexer schema
func (c *Client) RunMigration(ctx context.Context, migrationSQL string) error {
	_, err := c.IndexerPool.Exec(ctx, migrationSQL)
	if err != nil {
		return fmt.Errorf("failed to run migration: %w", err)
	}
	logrus.Info("Migration completed successfully on indexer schema")
	return nil
}

// LoadChains loads active chains from the backend database
func (c *Client) LoadChains(ctx context.Context) ([]ChainConfig, error) {
	query := fmt.Sprintf(`
		SELECT
			id,
			network_id,
			network,
			blockchain_network_type,
			is_active
		FROM %s.networks
		WHERE
			blockchain_network_type = 'EVM'
			AND is_active = true
		ORDER BY id
	`, c.BackendSchema)

	rows, err := c.ConfigPool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query chains: %w", err)
	}
	defer rows.Close()

	var chains []ChainConfig
	for rows.Next() {
		var chain ChainConfig
		var networkID *int32

		err := rows.Scan(
			&chain.ChainID,
			&networkID,
			&chain.Network,
			&chain.ChainType,
			&chain.IsActive,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan chain row: %w", err)
		}

		if networkID != nil {
			chain.NetworkID = int(*networkID)
		}

		chains = append(chains, chain)
	}

	return chains, nil
}

// LoadCurrencies loads active currencies for a specific chain
func (c *Client) LoadCurrencies(ctx context.Context, networkID string) ([]CurrencyConfig, error) {
	query := fmt.Sprintf(`
		SELECT
			id,
			contract_address,
			symbol,
			name,
			decimals,
			is_native,
			is_active,
			is_used_for_payment
		FROM %s.currencies
		WHERE
			network_id = $1
			AND is_active = true
			AND is_used_for_payment = true
		ORDER BY symbol
	`, c.BackendSchema)

	rows, err := c.ConfigPool.Query(ctx, query, networkID)
	if err != nil {
		return nil, fmt.Errorf("failed to query currencies: %w", err)
	}
	defer rows.Close()

	var currencies []CurrencyConfig
	for rows.Next() {
		var currency CurrencyConfig

		err := rows.Scan(
			&currency.ID,
			&currency.ContractAddress,
			&currency.Symbol,
			&currency.Name,
			&currency.Decimals,
			&currency.IsNative,
			&currency.IsActive,
			&currency.IsUsedForPayment,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan currency row: %w", err)
		}

		currencies = append(currencies, currency)
	}

	return currencies, nil
}

// LoadWallets loads active merchant wallets for a specific network
func (c *Client) LoadWallets(ctx context.Context, networkID string) ([]WalletConfig, error) {
	query := fmt.Sprintf(`
		SELECT
			id,
			address,
			type,
			status
		FROM %s.wallets
		WHERE
			network_id = $1
			AND type = 'MERCHANT_WALLET'
			AND status = 'ACTIVE'
		ORDER BY address
	`, c.BackendSchema)

	// Debug: Log the exact query being executed
	fmt.Printf("🔍 DEBUG: Loading wallets with network_id=%s from schema=%s\n", networkID, c.BackendSchema)

	rows, err := c.ConfigPool.Query(ctx, query, networkID)
	if err != nil {
		return nil, fmt.Errorf("failed to query wallets: %w", err)
	}
	defer rows.Close()

	var wallets []WalletConfig
	for rows.Next() {
		var wallet WalletConfig

		err := rows.Scan(
			&wallet.ID,
			&wallet.Address,
			&wallet.Type,
			&wallet.Status,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan wallet row: %w", err)
		}

		fmt.Printf("🔍 DEBUG: Loaded wallet: %s (%s) - status=%s\n", wallet.Address, wallet.Type, wallet.Status)
		wallets = append(wallets, wallet)
	}

	fmt.Printf("🔍 DEBUG: Total wallets loaded: %d\n", len(wallets))
	return wallets, nil
}

// ChainConfig represents chain configuration from database
type ChainConfig struct {
	ChainID   int    `json:"chain_id"`
	NetworkID int    `json:"network_id"`
	Network   string `json:"network"`
	ChainType string `json:"chain_type"`
	IsActive  bool   `json:"is_active"`
}

// CurrencyConfig represents currency configuration from database
type CurrencyConfig struct {
	ID               string `json:"id"`
	ContractAddress  string `json:"contract_address"`
	Symbol           string `json:"symbol"`
	Name             string `json:"name"`
	Decimals         int    `json:"decimals"`
	IsNative         bool   `json:"is_native"`
	IsActive         bool   `json:"is_active"`
	IsUsedForPayment bool   `json:"is_used_for_payment"`
}

// WalletConfig represents wallet configuration from database
type WalletConfig struct {
	ID     string `json:"id"`
	Address string `json:"address"`
	Type   string `json:"type"`
	Status string `json:"status"`
}