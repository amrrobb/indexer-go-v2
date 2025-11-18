package common

import (
	"flag"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

// CLIConfig holds common CLI configuration
type CLIConfig struct {
	ChainID int
	Logger  *logrus.Entry
}

// ParseCLIArguments parses common command line arguments and loads environment
func ParseCLIArguments(workerType string) (*CLIConfig, error) {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		// Don't return error, just warn - .env might not exist
		logrus.WithError(err).Warn("Warning: Could not load .env file")
	}

	// Initialize logger
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetLevel(logrus.InfoLevel)

	logger.WithField("component", "main").Infof("Starting indexer %s worker", workerType)

	// Parse command line flags
	var chainID int
	flag.IntVar(&chainID, "chain", 1, "Chain ID to index (default: 1 for Ethereum)")
	flag.Parse()

	logger.WithField("chain_id", chainID).Infof("Initializing %s worker", workerType)

	return &CLIConfig{
		ChainID: chainID,
		Logger:  logger.WithField("component", "cli"),
	}, nil
}