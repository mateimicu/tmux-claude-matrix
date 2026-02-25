package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/mateimicu/tmux-claude-matrix/internal/config"
	"github.com/mateimicu/tmux-claude-matrix/internal/hooks"
	"github.com/mateimicu/tmux-claude-matrix/internal/tmux"
	"github.com/mateimicu/tmux-claude-matrix/pkg/types"
)

func hookHandlerCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "hook-handler",
		Short:  "Handle Claude Code hook events (internal use)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(os.Stderr, "hook-handler: config load error: %v\n", err)
				cfg = &types.Config{StaleThreshold: 15 * time.Minute}
			}

			staleThreshold := cfg.StaleThreshold
			if staleThreshold <= 0 {
				staleThreshold = 15 * time.Minute
			}

			if err := hooks.HandleHookEvent(os.Stdin, tmux.New(), staleThreshold, nil); err != nil {
				fmt.Fprintf(os.Stderr, "hook-handler: %v\n", err)
				return err
			}
			return nil
		},
	}
}
