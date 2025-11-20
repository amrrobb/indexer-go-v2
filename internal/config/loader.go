package config

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
	"indexer-go-v2/internal/database"
	"indexer-go-v2/pkg/types"
)

// Loader handles loading configuration from backend database
type Loader struct {
	db     *database.Client
	cache  *ConfigCache
	logger *logrus.Entry
}

// ConfigCache represents cached configuration
type ConfigCache struct {
	Version     int64                           `json:"version"`
	Chains      map[int]*types.Chain           `json:"chains"`
	Currencies  map[int][]*types.Currency      `json:"currencies"`
	Wallets     map[int][]string               `json:"wallets"`
	WorkerConfigs map[int]*types.WorkerConfig `json:"worker_configs"`
	LastUpdate  time.Time                      `json:"last_update"`
}

// NewLoader creates a new configuration loader
func NewLoader(db *database.Client) *Loader {
	return &Loader{
		db:     db,
		logger: logrus.WithField("component", "config_loader"),
		cache: &ConfigCache{
			Chains:       make(map[int]*types.Chain),
			Currencies:   make(map[int][]*types.Currency),
			Wallets:      make(map[int][]string),
			WorkerConfigs: make(map[int]*types.WorkerConfig),
		},
	}
}

// LoadConfiguration loads all configuration from database
func (l *Loader) LoadConfiguration(ctx context.Context) (*ConfigCache, error) {
	l.logger.Info("Loading configuration from database")

	// Load chains
	chains, err := l.loadChains(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load chains: %w", err)
	}

	// Load currencies and wallets for each chain
	for _, chain := range chains {
		// Use the UUID network ID for database queries
		networkUUID := chain.ID // This is the UUID from the networks table

		// Load currencies
		currencies, err := l.db.LoadCurrencies(ctx, networkUUID)
		if err != nil {
			return nil, fmt.Errorf("failed to load currencies for chain %s: %w", networkUUID, err)
		}

		// Convert to types.Currency
		var currencyObjs []*types.Currency
		for _, curr := range currencies {
			currency := &types.Currency{
				ID:               curr.ID,
				NetworkID:        curr.ContractAddress, // Use contract address as network_id for compatibility
				ContractAddress:  curr.ContractAddress,
				Symbol:           curr.Symbol,
				Name:             curr.Name,
				Decimals:         curr.Decimals,
				IsNative:         curr.IsNative,
				IsActive:         curr.IsActive,
				IsUsedForPayment: curr.IsUsedForPayment,
			}
			currencyObjs = append(currencyObjs, currency)
		}

		// Load wallets
		walletConfigs, err := l.db.LoadWallets(ctx, networkUUID)
		if err != nil {
			return nil, fmt.Errorf("failed to load wallets for chain %s: %w", networkUUID, err)
		}

		// Extract addresses
		var addresses []string
		for _, wallet := range walletConfigs {
			addresses = append(addresses, wallet.Address)
		}

		// Store in cache
		chainID := chain.NetworkID // Use the numeric chain_id from the database
		l.cache.Chains[chainID] = chain
		l.cache.Currencies[chainID] = currencyObjs
		l.cache.Wallets[chainID] = addresses

		// Get chain-specific RPC URL
		rpcURL := l.getRPCURL(chain.Network)

		// Create worker config
		workerConfig := &types.WorkerConfig{
			ChainID:                chainID,
			Network:                chain.Network,
			ChainType:              chain.BlockchainNetworkType,
			ActiveCurrencies:       convertCurrencySlice(currencyObjs),
			WatchedAddresses:       addresses,
			ConfirmationsRequired: 5, // Default, will be overridden per chain
			RPCURL:                 rpcURL,
		}

		// Set chain-specific confirmation requirements
		if targetConfirmations, exists := types.ChainConfirmations[chain.Network]; exists {
			workerConfig.TargetConfirmations = targetConfirmations
		} else {
			workerConfig.TargetConfirmations = 12 // Default to Ethereum
		}

		l.cache.WorkerConfigs[chainID] = workerConfig

		l.logger.WithFields(logrus.Fields{
			"chain_id":     chainID,
			"network":      chain.Network,
			"currencies":   len(currencyObjs),
			"wallets":      len(addresses),
			"confirmations": workerConfig.TargetConfirmations,
		}).Info("Loaded chain configuration")
	}

	// Update cache metadata
	l.cache.LastUpdate = time.Now()
	l.cache.Version++ // Simple versioning

	l.logger.WithFields(logrus.Fields{
		"total_chains": len(chains),
		"total_currencies": l.getTotalCurrencies(),
		"total_wallets": l.getTotalWallets(),
	}).Info("Configuration loaded successfully")

	return l.cache, nil
}

// getRPCURL returns the RPC URL for a specific network
func (l *Loader) getRPCURL(network string) string {
	switch network {
	case "ethereum":
		return os.Getenv("ETHEREUM_RPC_URL")
	case "polygon", "polygon-pos":
		return os.Getenv("POLYGON_RPC_URL")
	case "arbitrum", "arbitrum-one":
		return os.Getenv("ARBITRUM_RPC_URL")
	case "bsc", "binance-smart-chain":
		return os.Getenv("BSC_RPC_URL")
	default:
		// Unknown network - return empty string
		return ""
	}
}

// GetWorkerConfig returns worker configuration for a specific chain
func (l *Loader) GetWorkerConfig(chainID int) (*types.WorkerConfig, error) {
	if config, exists := l.cache.WorkerConfigs[chainID]; exists {
		return config, nil
	}
	return nil, fmt.Errorf("worker configuration not found for chain ID %d", chainID)
}

// GetChain returns chain information
func (l *Loader) GetChain(chainID int) (*types.Chain, error) {
	if chain, exists := l.cache.Chains[chainID]; exists {
		return chain, nil
	}
	return nil, fmt.Errorf("chain not found for ID %d", chainID)
}

// CacheWorkerConfig stores a single worker configuration in the cache
func (l *Loader) CacheWorkerConfig(chainID int, workerConfig *types.WorkerConfig) {
	if l.cache.WorkerConfigs == nil {
		l.cache.WorkerConfigs = make(map[int]*types.WorkerConfig)
	}
	l.cache.WorkerConfigs[chainID] = workerConfig
}

// GetCache returns the current cached configuration
func (l *Loader) GetCache() *ConfigCache {
	return l.cache
}

// LoadChainConfiguration loads configuration for a specific chain only
func (l *Loader) LoadChainConfiguration(ctx context.Context, chainID int) (*types.WorkerConfig, error) {
	l.logger.WithField("chain_id", chainID).Info("Loading single chain configuration from database")

	// Load only the specific chain
	chain, err := l.loadChain(ctx, chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to load chain %d: %w", chainID, err)
	}

	// Use the UUID network ID for database queries
	networkUUID := chain.ID // This is the UUID from the networks table

	// Load currencies for this chain
	currencies, err := l.db.LoadCurrencies(ctx, networkUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to load currencies for chain %s: %w", networkUUID, err)
	}

	// Convert to types.Currency
	var currencyObjs []*types.Currency
	for _, curr := range currencies {
		currency := &types.Currency{
			ID:               curr.ID,
			NetworkID:        curr.ContractAddress, // Use contract address as network_id for compatibility
			ContractAddress:  curr.ContractAddress,
			Symbol:           curr.Symbol,
			Name:             curr.Name,
			Decimals:         curr.Decimals,
			IsNative:         curr.IsNative,
			IsActive:         curr.IsActive,
			IsUsedForPayment: curr.IsUsedForPayment,
		}
		currencyObjs = append(currencyObjs, currency)
	}

	// Load wallets for this chain
	walletConfigs, err := l.db.LoadWallets(ctx, networkUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to load wallets for chain %s: %w", networkUUID, err)
	}

	// Extract addresses
	var addresses []string
	for _, wallet := range walletConfigs {
		addresses = append(addresses, wallet.Address)
	}

	// Debug: Log loaded addresses for comparison
	maxShow := 5
	if len(addresses) < maxShow {
		maxShow = len(addresses)
	}
	l.logger.WithFields(logrus.Fields{
		"chain_id":      chainID,
		"network_uuid":  networkUUID,
		"wallets_count": len(walletConfigs),
		"addresses":     addresses[:maxShow], // Show first 5
	}).Info("🔍 DEBUG: Loaded wallet addresses from database")

	// Get chain-specific RPC URL
	rpcURL := l.getRPCURL(chain.Network)

	// Create worker config
	workerConfig := &types.WorkerConfig{
		ChainID:                chainID,
		Network:                chain.Network,
		ChainType:              chain.BlockchainNetworkType,
		ActiveCurrencies:       convertCurrencySlice(currencyObjs),
		WatchedAddresses:       addresses,
		ConfirmationsRequired: 5, // Default, will be overridden per chain
		RPCURL:                 rpcURL,
	}

	// Set chain-specific confirmation requirements
	if targetConfirmations, exists := types.ChainConfirmations[chain.Network]; exists {
		workerConfig.TargetConfirmations = targetConfirmations
	}

	l.logger.WithFields(logrus.Fields{
		"chain_id":         chainID,
		"network":          chain.Network,
		"currencies_count": len(currencyObjs),
		"wallets_count":    len(addresses),
		"rpc_url":          rpcURL,
	}).Info("Single chain configuration loaded successfully")

	return workerConfig, nil
}

// loadChain loads a single chain from database by chain_id
func (l *Loader) loadChain(ctx context.Context, chainID int) (*types.Chain, error) {
	query := fmt.Sprintf(`
		SELECT
			id,
			chain_id,
			network,
			blockchain_network_type,
			is_active
		FROM %s.networks
		WHERE chain_id = $1
	`, l.db.BackendSchema)

	row := l.db.ConfigPool.QueryRow(ctx, query, fmt.Sprintf("%d", chainID))

	var chain types.Chain
	var networkID string

	err := row.Scan(
		&chain.ID,
		&networkID,
		&chain.Network,
		&chain.BlockchainNetworkType,
		&chain.IsActive,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to load chain %d: %w", chainID, err)
	}

	// Convert string networkID to int
	if networkID != "" {
		if parsed, err := strconv.Atoi(networkID); err == nil {
			chain.NetworkID = parsed
		}
	}

	return &chain, nil
}

// RefreshConfiguration reloads configuration from database
func (l *Loader) RefreshConfiguration(ctx context.Context) error {
	l.logger.Info("Refreshing configuration from database")
	newCache, err := l.LoadConfiguration(ctx)
	if err != nil {
		return fmt.Errorf("failed to refresh configuration: %w", err)
	}

	l.cache = newCache
	l.logger.Info("Configuration refreshed successfully")
	return nil
}

// StartConfigWatcher starts a background goroutine to periodically refresh configuration
func (l *Loader) StartConfigWatcher(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	l.logger.WithField("interval", interval).Info("Starting configuration watcher")

	for {
		select {
		case <-ctx.Done():
			l.logger.Info("Configuration watcher stopped")
			return
		case <-ticker.C:
			if err := l.RefreshConfiguration(ctx); err != nil {
				l.logger.WithError(err).Error("Failed to refresh configuration")
			}
		}
	}
}

// loadChains loads chains from database and converts to types.Chain
func (l *Loader) loadChains(ctx context.Context) ([]*types.Chain, error) {
	query := fmt.Sprintf(`
		SELECT
			id,
			chain_id,
			network,
			blockchain_network_type,
			is_active
		FROM %s.networks
		WHERE
			blockchain_network_type = 'EVM'
			AND is_active = true
		ORDER BY id
	`, l.db.BackendSchema)

	rows, err := l.db.ConfigPool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query chains: %w", err)
	}
	defer rows.Close()

	var chains []*types.Chain
	for rows.Next() {
		var chain types.Chain
		var chainIDText string

		err := rows.Scan(
			&chain.ID,
			&chainIDText,
			&chain.Network,
			&chain.BlockchainNetworkType,
			&chain.IsActive,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan chain row: %w", err)
		}

		// Convert chain_id text to int
		if chainIDText != "" {
			if chainIDInt, err := strconv.Atoi(chainIDText); err == nil {
				chain.NetworkID = chainIDInt
			}
		}

		chains = append(chains, &chain)
	}

	return chains, nil
}

// getTotalCurrencies returns total number of currencies across all chains
func (l *Loader) getTotalCurrencies() int {
	total := 0
	for _, currencies := range l.cache.Currencies {
		total += len(currencies)
	}
	return total
}

// getTotalWallets returns total number of wallets across all chains
func (l *Loader) getTotalWallets() int {
	total := 0
	for _, wallets := range l.cache.Wallets {
		total += len(wallets)
	}
	return total
}

// GetActiveChainIDs returns a slice of active chain IDs
func (l *Loader) GetActiveChainIDs() []int {
	var chainIDs []int
	for chainID := range l.cache.WorkerConfigs {
		chainIDs = append(chainIDs, chainID)
	}
	return chainIDs
}

// ValidateConfiguration validates the loaded configuration
func (l *Loader) ValidateConfiguration() error {
	if len(l.cache.WorkerConfigs) == 0 {
		return fmt.Errorf("no worker configurations loaded")
	}

	// Validate each worker config
	for chainID, config := range l.cache.WorkerConfigs {
		if config.ChainID != chainID {
			return fmt.Errorf("chain ID mismatch for worker config: expected %d, got %d", chainID, config.ChainID)
		}

		if config.RPCURL == "" {
			return fmt.Errorf("RPC URL not configured for chain %d", chainID)
		}

		if len(config.ActiveCurrencies) == 0 {
			return fmt.Errorf("no active currencies found for chain %d", chainID)
		}

		if len(config.WatchedAddresses) == 0 {
			return fmt.Errorf("no watched addresses found for chain %d", chainID)
		}

		// Validate currency addresses
		for _, currency := range config.ActiveCurrencies {
			if !currency.IsNative && currency.ContractAddress == "" {
				return fmt.Errorf("non-native currency %s has no contract address", currency.Symbol)
			}
		}
	}

	l.logger.Info("Configuration validation passed")
	return nil
}

// convertChainIDToInt converts chain ID string to int
func (l *Loader) convertChainIDToInt(chainID string) int {
	if id, err := strconv.Atoi(chainID); err == nil {
		return id
	}
	l.logger.WithField("chain_id", chainID).Warn("Invalid chain ID, defaulting to 0")
	return 0
}

// convertCurrencySlice converts []*types.Currency to []types.Currency
func convertCurrencySlice(currencies []*types.Currency) []types.Currency {
	result := make([]types.Currency, len(currencies))
	for i, c := range currencies {
		result[i] = *c
	}
	return result
}