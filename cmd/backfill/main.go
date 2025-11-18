package main

import (
	"log"

	"indexer-go-v2/internal/worker"
)

func main() {
	if err := worker.RunWorker(worker.BackfillType); err != nil {
		log.Fatalf("Backfill worker failed: %v", err)
	}
}