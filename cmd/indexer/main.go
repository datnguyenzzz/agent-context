package main

// ponytail: codebase indexer walks directories, filters Go and Terraform files, splits them into AST chunks, embeds, and stores in DuckDB using a Merkle Tree

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"agent-mem/internal/merkle"
)

func main() {
	targetDir := "."
	if len(os.Args) > 1 {
		targetDir = os.Args[1]
	}

	absPath, err := filepath.Abs(targetDir)
	if err != nil {
		log.Fatalf("Failed to resolve absolute path of %s: %v", targetDir, err)
	}

	fmt.Printf("Indexing codebase: %s (Go & Terraform only)\n", absPath)

	added, modified, deleted, err := merkle.UpdateIndex(absPath)
	if err != nil {
		log.Fatalf("Failed to index codebase: %v", err)
	}

	if added == 0 && modified == 0 && deleted == 0 {
		fmt.Println("✓ Codebase index is already up to date!")
	} else {
		fmt.Printf("\nIncremental indexing completed: %d files added, %d modified, %d deleted.\n", added, modified, deleted)
	}
}
