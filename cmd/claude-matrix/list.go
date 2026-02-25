package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mateimicu/tmux-claude-matrix/internal/config"
	"github.com/mateimicu/tmux-claude-matrix/internal/fzf"
	"github.com/mateimicu/tmux-claude-matrix/internal/session"
	"github.com/mateimicu/tmux-claude-matrix/internal/status"
	"github.com/mateimicu/tmux-claude-matrix/internal/tmux"
	"github.com/mateimicu/tmux-claude-matrix/pkg/types"
)

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List and switch to existing sessions",
		Long:  `List all managed tmux sessions and switch to one.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.Context())
		},
	}
}

func runList(_ context.Context) error {
	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	sessionMgr := session.NewManager(cfg.SessionsDir)
	tmuxMgr := tmux.New()

	// Main loop - continue showing list until user exits or switches
	for {
		// Load sessions
		sessions, err := sessionMgr.List()
		if err != nil {
			return fmt.Errorf("failed to list sessions: %w", err)
		}

		if len(sessions) == 0 {
			fmt.Println("No sessions found. Create one with: claude-matrix create")
			fmt.Print("\nPress Enter to close...")
			//nolint:errcheck // intentionally ignoring - just waiting for keypress
			bufio.NewReader(os.Stdin).ReadBytes('\n')
			return nil
		}

		// Get tmux status
		activeSessions, err := tmuxMgr.ListSessions()
		if err != nil {
			return fmt.Errorf("failed to list tmux sessions: %w", err)
		}
		activeMap := make(map[string]bool)
		for _, name := range activeSessions {
			activeMap[name] = true
		}

		// Build session status list
		var statusList []*types.SessionStatus
		for _, sess := range sessions {
			sessStatus := &types.SessionStatus{
				Session:       sess,
				TmuxActive:    activeMap[sess.Name],
				ClaudeRunning: false,
				ClaudeState:   types.ClaudeStateStopped,
			}

			// Check Claude status if session is active
			if sessStatus.TmuxActive {
				state, lastActivity := tmuxMgr.GetDetailedClaudeState(sess.Name, cfg.StaleThreshold)
				sessStatus.ClaudeState = state
				sessStatus.LastActivity = lastActivity
				sessStatus.ClaudeRunning = state == types.ClaudeStateRunning ||
					state == types.ClaudeStateWaitingForInput ||
					state == types.ClaudeStateIdle
			}

			statusList = append(statusList, sessStatus)
		}

		// Show FZF selection with action support
		selection, err := fzf.SelectSessionWithAction(statusList)
		if err != nil {
			return fmt.Errorf("session selection cancelled: %w", err)
		}

		// Handle action
		switch selection.Action {
		case fzf.SessionActionDelete:
			if err := handleDeleteAction(sessionMgr, tmuxMgr, selection.Session); err != nil {
				fmt.Printf("⚠️  Failed to delete session: %v\n", err)
			}
			// Continue loop to show updated list

		case fzf.SessionActionSwitch:
			if err := handleSwitchAction(cfg, tmuxMgr, selection.Session); err != nil {
				return err
			}
			// Exit after switching
			return nil

		default:
			return fmt.Errorf("session selection cancelled")
		}
	}
}

func handleDeleteAction(sessionMgr *session.Manager, tmuxMgr *tmux.Manager, selected *types.SessionStatus) error {
	sess := selected.Session

	// Ask for confirmation
	fmt.Printf("\n🗑️  Delete session '%s'? (y/N): ", sess.Name)
	var confirmation string
	if _, err := fmt.Scanln(&confirmation); err != nil {
		// Treat read errors as cancellation
		confirmation = "n"
	}

	if confirmation != "y" && confirmation != "Y" {
		fmt.Println("Deletion cancelled.")
		return nil
	}

	// Kill tmux session if active
	if tmuxMgr.SessionExists(sess.Name) {
		fmt.Printf("🛑 Killing tmux session '%s'...\n", sess.Name)
		if err := tmuxMgr.KillSession(sess.Name); err != nil {
			fmt.Printf("⚠️  Failed to kill tmux session: %v\n", err)
		}
	}

	// Delete metadata
	if err := sessionMgr.Delete(sess.Name); err != nil {
		return fmt.Errorf("failed to delete session metadata: %w", err)
	}

	// Clean up status file
	status.RemoveState(status.DefaultStatusDir(), sess.Name) //nolint:errcheck // Best-effort cleanup

	fmt.Printf("✓ Session '%s' deleted successfully!\n\n", sess.Name)
	return nil
}

func handleSwitchAction(cfg *types.Config, tmuxMgr *tmux.Manager, selected *types.SessionStatus) error {
	// Switch to session
	fmt.Printf("🚀 Switching to session '%s'...\n", selected.Session.Name)

	// If session is not active, recreate it
	if !selected.TmuxActive {
		fmt.Println("⚠️  Session not active, recreating...")

		var claudeCmd string
		if cfg.ClaudeBin != "" {
			claudeCmd = cfg.ClaudeBin + " " + strings.Join(cfg.ClaudeArgs, " ")
		}

		if err := tmuxMgr.CreateSession(selected.Session.Name, selected.Session.ClonePath, claudeCmd); err != nil {
			return fmt.Errorf("failed to recreate session: %w", err)
		}
	}

	// Set title env var so the status bar picks it up
	if selected.Session.Title != "" {
		if err := tmuxMgr.SetSessionEnv(selected.Session.Name, "@claude-matrix-title", selected.Session.Title); err != nil {
			fmt.Printf("⚠️  Failed to set session title: %v\n", err)
		}
	}

	if err := tmuxMgr.SwitchToSession(selected.Session.Name); err != nil {
		fmt.Printf("⚠️  Failed to switch to session: %v\n", err)
		fmt.Printf("You can attach manually with: tmux attach -t %s\n", selected.Session.Name)
	}

	return nil
}
