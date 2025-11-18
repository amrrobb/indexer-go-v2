package common

import (
	"context"

	"indexer-go-v2/internal/config"
	"indexer-go-v2/internal/rpc"
	"indexer-go-v2/pkg/types"
)

// LoadAndCacheConfig loads single-chain configuration and caches it for workers
func LoadAndCacheConfig(configLoader *config.Loader, ctx context.Context, chainID int) (*types.WorkerConfig, error) {
	// Load only the specific chain configuration we need
	workerConfig, err := configLoader.LoadChainConfiguration(ctx, chainID)
	if err != nil {
		return nil, err
	}

	// Cache the loaded configuration so the worker can find it
	configLoader.CacheWorkerConfig(chainID, workerConfig)

	return workerConfig, nil
}

// InitializeRPCClient creates RPC client from worker configuration
func InitializeRPCClient(workerConfig *types.WorkerConfig) (*rpc.Client, error) {
	return rpc.NewClient(workerConfig.RPCURL, ""), nil
}