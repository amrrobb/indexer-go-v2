package main

import (
	"context"
	"fmt"
	"log"

	"indexer-go-v2/internal/config"
	"indexer-go-v2/internal/database"
)

func main() {
	ctx := context.Background()

	// Initialize database
	db, err := database.NewClient()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Initialize config loader
	configLoader := config.NewLoader(db)

	// Load configuration
	config, err := configLoader.LoadConfiguration(ctx)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Check chain 42161 (Arbitrum)
	chainID := 42161
	if chain, exists := config.Chains[chainID]; exists {
		fmt.Printf("✅ Chain %d found:\n", chainID)
		fmt.Printf("   Network: %s\n", chain.Network)
		fmt.Printf("   Type: %s\n", chain.BlockchainNetworkType)
		fmt.Printf("   Is Active: %t\n", chain.IsActive)
	} else {
		fmt.Printf("❌ Chain %d NOT found\n", chainID)
		return
	}

	// Check worker config for chain 42161
	workerConfig, err := configLoader.GetWorkerConfig(chainID)
	if err != nil {
		log.Fatalf("Failed to get worker config: %v", err)
	}

	fmt.Printf("\n✅ Worker Config for chain %d:\n", chainID)
	fmt.Printf("   Network: %s\n", workerConfig.Network)
	fmt.Printf("   Target Confirmations: %d\n", workerConfig.TargetConfirmations)
	fmt.Printf("   Active Currencies: %d\n", len(workerConfig.ActiveCurrencies))
	fmt.Printf("   Watched Addresses: %d\n", len(workerConfig.WatchedAddresses))

	// List active currencies
	fmt.Printf("\n📊 Active Currencies:\n")
	for i, currency := range workerConfig.ActiveCurrencies {
		fmt.Printf("   %d. %s (%s) - %s\n", i+1, currency.Symbol, currency.Name, currency.ContractAddress)
		if !currency.IsNative {
			fmt.Printf("      Contract: %s\n", currency.ContractAddress)
		}
		fmt.Printf("      Decimals: %d\n", currency.Decimals)
		fmt.Printf("      Active: %t\n", currency.IsActive)
	}

	// List first few watched addresses
	fmt.Printf("\n👀 Watched Addresses (first 10 of %d):\n", len(workerConfig.WatchedAddresses))
	maxAddresses := 10
	if len(workerConfig.WatchedAddresses) < maxAddresses {
		maxAddresses = len(workerConfig.WatchedAddresses)
	}
	for i := 0; i < maxAddresses; i++ {
		fmt.Printf("   %d. %s\n", i+1, workerConfig.WatchedAddresses[i])
	}
	if len(workerConfig.WatchedAddresses) > maxAddresses {
		fmt.Printf("   ... and %d more\n", len(workerConfig.WatchedAddresses)-maxAddresses)
	}

	fmt.Printf("\n🎯 Configuration Status: %s\n", "READY FOR INDEXING")
}