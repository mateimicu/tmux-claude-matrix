package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mateimicu/tmux-claude-matrix/internal/fzf"
	"github.com/mateimicu/tmux-claude-matrix/internal/logging"
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

func runList(ctx context.Context) error {
	cfg := configFromContext(ctx)
	log := loggerFromContext(ctx)

	sessionMgr := session.NewManager(cfg.SessionsDir)
	tmuxMgr := tmux.New()

	// Toggle state for hiding inactive sessions (resets each invocation)
	showActiveOnly := false

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
				sessStatus.ClaudeRunning = tmuxMgr.GetClaudeStatus(sess.Name)
				// Get detailed state
				state, lastActivity := tmuxMgr.GetDetailedClaudeState(sess.Name)
				sessStatus.ClaudeState = state
				sessStatus.LastActivity = lastActivity
			}

			statusList = append(statusList, sessStatus)
		}

		// Apply active-only filter if toggled on
		displayList := statusList
		if showActiveOnly {
			filtered := fzf.FilterActiveSessions(statusList)
			if len(filtered) == 0 {
				showActiveOnly = false
				log.Warnf("⚠️  No active sessions to filter, showing all sessions.\n")
			} else {
				displayList = filtered
			}
		}

		// Show FZF selection with action support
		selection, err := fzf.SelectSessionWithAction(displayList, showActiveOnly)
		if err != nil {
			return fmt.Errorf("session selection cancelled: %w", err)
		}

		// Handle action
		switch selection.Action {
		case fzf.SessionActionToggleFilter:
			showActiveOnly = !showActiveOnly
			continue

		case fzf.SessionActionTools:
			if err := handleToolsAction(ctx, cfg); err != nil {
				fmt.Printf("⚠️  Tools action failed: %v\n", err)
			}
			continue

		case fzf.SessionActionDelete:
			if err := handleDeleteAction(sessionMgr, tmuxMgr, selection.Session, log); err != nil {
				log.Warnf("⚠️  Failed to delete session: %v\n", err)
			}
			// Continue loop to show updated list

		case fzf.SessionActionRename:
			if err := handleRenameAction(sessionMgr, tmuxMgr, selection.Session); err != nil {
				fmt.Printf("⚠️  Failed to rename session: %v\n", err)
			}
			// Continue loop to show updated list

		case fzf.SessionActionSwitch:
			if err := handleSwitchAction(cfg, tmuxMgr, selection.Session, log); err != nil {
				return err
			}
			// Exit after switching
			return nil

		default:
			return fmt.Errorf("session selection cancelled")
		}
	}
}

func handleToolsAction(ctx context.Context, cfg *types.Config) error {
	action, err := fzf.SelectToolAction()
	if err != nil {
		return err
	}

	switch action {
	case fzf.ToolActionPrefillCache:
		return runPrefillCache(ctx, cfg)
	default:
		return nil
	}
}

func handleDeleteAction(sessionMgr *session.Manager, tmuxMgr *tmux.Manager, selected *types.SessionStatus, log *logging.Logger) error {
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
		log.Debugf("🛑 Killing tmux session '%s'...\n", sess.Name)
		if err := tmuxMgr.KillSession(sess.Name); err != nil {
			log.Warnf("⚠️  Failed to kill tmux session: %v\n", err)
		}
	}

	// Delete metadata
	if err := sessionMgr.Delete(sess.Name); err != nil {
		return fmt.Errorf("failed to delete session metadata: %w", err)
	}

	// Clean up status files (aggregate + per-agent)
	statusDir := status.DefaultStatusDir()
	status.RemoveAllAgentStates(statusDir, sess.Name) //nolint:errcheck // Best-effort cleanup
	status.RemoveState(statusDir, sess.Name)          //nolint:errcheck // Best-effort cleanup

	// User-facing success confirmation — always visible
	fmt.Printf("✓ Session '%s' deleted successfully!\n\n", sess.Name)
	return nil
}

func handleSwitchAction(cfg *types.Config, tmuxMgr *tmux.Manager, selected *types.SessionStatus, log *logging.Logger) error {
	// Switch to session
	log.Debugf("🚀 Switching to session '%s'...\n", selected.Session.Name)

	// If session is not active, recreate it
	if !selected.TmuxActive {
		log.Warnf("⚠️  Session not active, recreating...\n")

		var claudeCmd string
		if cfg.ClaudeBin != "" {
			claudeCmd = cfg.ClaudeBin + " " + strings.Join(cfg.ClaudeArgs, " ")
		}

		if err := tmuxMgr.CreateSession(selected.Session.Name, selected.Session.WorktreePath, claudeCmd); err != nil {
			return fmt.Errorf("failed to recreate session: %w", err)
		}
	}

	// Set title env var so the status bar picks it up
	if selected.Session.Title != "" {
		if err := tmuxMgr.SetSessionEnv(selected.Session.Name, "@claude-matrix-title", selected.Session.Title); err != nil {
			log.Warnf("⚠️  Failed to set session title: %v\n", err)
		}
	}

	if err := tmuxMgr.SwitchToSession(selected.Session.Name); err != nil {
		log.Warnf("⚠️  Failed to switch to session: %v\n", err)
		log.Warnf("You can attach manually with: tmux attach -t %s\n", selected.Session.Name)
	}

	return nil
}

func handleRenameAction(sessionMgr *session.Manager, tmuxMgr *tmux.Manager, selected *types.SessionStatus) error {
	fmt.Printf("\n✏️  Rename session '%s' (current title: %q)\n", selected.Session.Name, selected.Session.Title)
	fmt.Print("Enter new title (empty to cancel): ")

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return nil
	}
	newTitle := strings.TrimSpace(scanner.Text())
	if newTitle == "" {
		fmt.Println("Rename cancelled.")
		return nil
	}

	// Update title in session metadata
	selected.Session.Title = newTitle
	if err := sessionMgr.Save(selected.Session); err != nil {
		return fmt.Errorf("failed to save session metadata: %w", err)
	}

	// Update tmux env var for status bar
	if err := tmuxMgr.SetSessionEnv(selected.Session.Name, "@claude-matrix-title", newTitle); err != nil {
		fmt.Printf("⚠️  Failed to update tmux env var: %v\n", err)
	}

	fmt.Printf("✓ Session '%s' renamed to '%s'\n\n", selected.Session.Name, newTitle)
	return nil
}
