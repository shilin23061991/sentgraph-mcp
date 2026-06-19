package main

import (
	"context"
	"fmt"
	"os"

	"github.com/shilin23061991/sentgraph-mcp/internal/config"
	"github.com/shilin23061991/sentgraph-mcp/internal/hooks"
	"github.com/shilin23061991/sentgraph-mcp/internal/memory"
	"github.com/shilin23061991/sentgraph-mcp/internal/zepstore"
)

// runHook handles a single Claude Code lifecycle event. The hook payload is
// read as JSON from stdin; read hooks emit additionalContext on stdout.
func runHook(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("hook requires an event name (e.g. SessionStart, UserPromptSubmit, Stop)")
	}
	event := args[0]
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return err
	}
	store := zepstore.New(cfg.ZepAPIKey)
	return hooks.New(memory.New(cfg, store)).Handle(ctx, event, os.Stdin, os.Stdout)
}
