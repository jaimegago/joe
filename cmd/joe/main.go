package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/joe"
	"github.com/jaimegago/joe/internal/repl"
)

func main() {
	ctx := context.Background()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// TODO: Initialize LLM adapter based on config
	// TODO: Initialize graph store (Cayley)
	// TODO: Initialize SQL store (SQLite)

	// For now, we'll just print a message since we don't have implementations yet
	fmt.Println("Joe scaffolding is ready!")
	fmt.Printf("Configuration loaded: LLM provider=%s, model=%s\n", cfg.LLM.Provider, cfg.LLM.Model)
	fmt.Println()
	fmt.Println("To complete Phase 1, we still need to:")
	fmt.Println("  - Implement LLM adapter (starting with Claude)")
	fmt.Println("  - Implement SQL store with migrations")
	fmt.Println("  - Implement graph store (Cayley)")
	fmt.Println()

	// When implementations are ready, the flow will be:
	// 1. Create Joe instance
	// joeInstance := joe.New(cfg, llmAdapter, graphStore, sqlStore)
	//
	// 2. Start background services
	// if err := joeInstance.Start(ctx); err != nil {
	//     log.Fatalf("Failed to start Joe: %v", err)
	// }
	//
	// 3. Run REPL
	// replInstance := repl.New(joeInstance)
	// if err := replInstance.Run(ctx); err != nil {
	//     log.Fatalf("REPL failed: %v", err)
	// }

	_ = ctx
	_ = joe.New
	_ = repl.New

	os.Exit(0)
}
