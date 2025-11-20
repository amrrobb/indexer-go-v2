package main

import (
	"indexer-go-v2/internal/common"
	"indexer-go-v2/internal/config"
	"indexer-go-v2/internal/worker"
)

func main() {
	// Parse CLI arguments and load environment
	cliConfig, err := common.ParseCLIArguments("forward")
	if err != nil {
		cliConfig.Logger.WithError(err).Fatal("Failed to parse CLI arguments")
	}

	// Initialize common components
	components, err := common.InitializeComponents("forward")
	if err != nil {
		components.Logger.WithError(err).Fatal("Failed to initialize components")
	}
	defer components.CloseComponents()

	// Create context using shutdown manager
	shutdownManager := common.NewShutdownManager("forward")
	shutdownManager.SetupSignalHandling()

	// Load configuration
	configLoader := config.NewLoader(components.DB)
	workerConfig, err := common.LoadAndCacheConfig(configLoader, shutdownManager.GetContext(), cliConfig.ChainID)
	if err != nil {
		components.Logger.WithError(err).Fatal("Failed to load worker configuration")
	}

	// Initialize RPC client
	rpcClient, err := common.InitializeRPCClient(workerConfig)
	if err != nil {
		components.Logger.WithError(err).Fatal("Failed to initialize RPC client")
	}

	// Create forward worker
	forwardWorker, err := worker.NewForwardWorker(
		configLoader,
		components.DB,
		components.Redis,
		rpcClient,
		components.Webhook,
		cliConfig.ChainID,
	)
	if err != nil {
		components.Logger.WithError(err).Fatal("Failed to create forward worker")
	}

	components.Logger.Info("Forward worker initialized successfully")

	// Start health check server
	healthServer := common.NewHealthServer()
	healthServer.Start()
	defer healthServer.Stop(shutdownManager.GetContext())

	// Start worker in goroutine with shutdown handling
	go func() {
		if err := forwardWorker.Start(shutdownManager.GetContext()); err != nil {
			shutdownManager.SendError(err)
		}
	}()

	// Wait for shutdown signal or error
	if err := shutdownManager.WaitForShutdown(forwardWorker); err != nil {
		components.Logger.WithError(err).Error("Worker failed unexpectedly")
	}
}