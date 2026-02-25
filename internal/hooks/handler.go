package hooks

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mateimicu/tmux-claude-matrix/internal/status"
	"github.com/mateimicu/tmux-claude-matrix/internal/tmux"
	"github.com/mateimicu/tmux-claude-matrix/pkg/types"
)

// HookEvent represents a Claude Code hook event received via stdin.
type HookEvent struct {
	HookEventName    string `json:"hook_event_name"`
	NotificationType string `json:"notification_type,omitempty"`
	SessionID        string `json:"session_id"`
}

// Logger is an interface for debug logging.
type Logger interface {
	Printf(format string, v ...interface{})
}

// MapEventToState maps a hook event to its corresponding ClaudeState.
func MapEventToState(event *HookEvent) types.ClaudeState {
	switch event.HookEventName {
	case "SessionStart":
		return types.ClaudeStateIdle
	case "UserPromptSubmit":
		return types.ClaudeStateRunning
	case "PreToolUse":
		return types.ClaudeStateRunning
	case "Stop":
		return types.ClaudeStateIdle
	case "Notification":
		switch event.NotificationType {
		case "permission_prompt", "elicitation_dialog":
			return types.ClaudeStateWaitingForInput
		case "idle_prompt":
			return types.ClaudeStateIdle
		default:
			return types.ClaudeStateUnknown
		}
	case "SessionEnd":
		return types.ClaudeStateStopped
	default:
		return types.ClaudeStateUnknown
	}
}

// HandleHookEvent reads a hook event from stdin and updates tmux state accordingly.
// It uses per-agent state tracking keyed by the event's session_id.
func HandleHookEvent(reader io.Reader, mgr *tmux.Manager, staleThreshold time.Duration, logger Logger) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	var event HookEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}

	state := MapEventToState(&event)

	if logger != nil {
		logger.Printf("event=%s session_id=%s state=%s", event.HookEventName, event.SessionID, state)
	}

	tmuxPane := os.Getenv("TMUX_PANE")
	if tmuxPane == "" {
		return fmt.Errorf("TMUX_PANE environment variable is not set; hook handler requires a tmux pane context")
	}

	sessionName, err := mgr.GetSessionNameFromPane(tmuxPane)
	if err != nil {
		return err
	}

	if logger != nil {
		logger.Printf("tmux_pane=%s session_name=%s", tmuxPane, sessionName)
	}

	statusDir := status.DefaultStatusDir()

	if state == types.ClaudeStateStopped {
		if err := status.RemoveAgentState(statusDir, sessionName, event.SessionID); err != nil {
			return err
		}

		// Read remaining state to compute aggregate for window name
		sf, err := status.ReadStateFile(statusDir, sessionName)
		if err != nil {
			// File was removed (last agent) — show stopped
			if logger != nil {
				logger.Printf("last agent removed, setting window to stopped")
			}
			return mgr.RenameWindowByPane(tmuxPane, status.EmojiForState(types.ClaudeStateStopped)+"claude")
		}

		aggregate, _ := status.ComputeState(sf, staleThreshold)
		if logger != nil {
			logger.Printf("aggregate_state=%s (after agent removal)", aggregate)
		}
		return mgr.RenameWindowByPane(tmuxPane, status.EmojiForState(aggregate)+"claude")
	}

	if err := status.UpdateAgentState(statusDir, sessionName, event.SessionID, state); err != nil {
		return err
	}

	// Compute aggregate state for window name
	sf, err := status.ReadStateFile(statusDir, sessionName)
	if err != nil {
		return err
	}

	aggregate, _ := status.ComputeState(sf, staleThreshold)
	if logger != nil {
		logger.Printf("aggregate_state=%s", aggregate)
	}
	return mgr.RenameWindowByPane(tmuxPane, status.EmojiForState(aggregate)+"claude")
}
