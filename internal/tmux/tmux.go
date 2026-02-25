package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mateimicu/tmux-claude-matrix/internal/status"
	"github.com/mateimicu/tmux-claude-matrix/pkg/types"
)

// Manager handles tmux operations
type Manager struct{}

// New creates a new tmux Manager
func New() *Manager {
	return &Manager{}
}

// CreateSession creates a new tmux session
func (m *Manager) CreateSession(name, path, command string) error {
	args := []string{"new-session", "-d", "-s", name, "-c", path}
	if command != "" {
		args = append(args, command)
	}
	cmd := exec.Command("tmux", args...)
	return cmd.Run()
}

// CreateSessionWithCommand creates a new tmux session and runs a command in the first window
func (m *Manager) CreateSessionWithCommand(name, path, command string) error {
	args := []string{"new-session", "-d", "-s", name, "-c", path}
	if command != "" {
		args = append(args, command)
	}
	cmd := exec.Command("tmux", args...)
	return cmd.Run()
}

// CreateWindow creates a window in a session
func (m *Manager) CreateWindow(session, name, command, path string) error {
	args := []string{"new-window", "-t", session + ":", "-n", name}
	if path != "" {
		args = append(args, "-c", path)
	}
	if command != "" {
		args = append(args, command)
	}

	cmd := exec.Command("tmux", args...)
	return cmd.Run()
}

// SessionExists checks if a tmux session exists
func (m *Manager) SessionExists(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// KillSession kills a tmux session
func (m *Manager) KillSession(name string) error {
	cmd := exec.Command("tmux", "kill-session", "-t", name)
	return cmd.Run()
}

// SwitchToSession attaches or switches to a session
func (m *Manager) SwitchToSession(name string) error {
	if os.Getenv("TMUX") != "" {
		// Inside tmux, switch client
		cmd := exec.Command("tmux", "switch-client", "-t", name)
		return cmd.Run()
	}
	// Outside tmux, attach
	cmd := exec.Command("tmux", "attach-session", "-t", name)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SetSessionEnv sets a session-level environment variable
func (m *Manager) SetSessionEnv(session, key, value string) error {
	cmd := exec.Command("tmux", "set-environment", "-t", session, key, value)
	return cmd.Run()
}

// GetSessionEnv gets a session-level environment variable
func (m *Manager) GetSessionEnv(session, key string) (string, error) {
	cmd := exec.Command("tmux", "show-environment", "-t", session, key)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	// Output format: "KEY=VALUE\n"
	line := strings.TrimSpace(string(output))
	if _, value, ok := strings.Cut(line, "="); ok {
		return value, nil
	}
	return "", fmt.Errorf("unexpected format: %s", line)
}

// ListSessions returns all tmux session names
func (m *Manager) ListSessions() ([]string, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	output, err := cmd.Output()
	if err != nil {
		// No sessions exist
		if strings.Contains(err.Error(), "no server running") {
			return nil, nil
		}
		return nil, err
	}

	sessions := strings.Split(strings.TrimSpace(string(output)), "\n")
	var result []string
	for _, s := range sessions {
		if s != "" {
			result = append(result, s)
		}
	}

	return result, nil
}

// SelectWindow selects a window in the session
func (m *Manager) SelectWindow(session, window string) error {
	cmd := exec.Command("tmux", "select-window", "-t", fmt.Sprintf("%s:%s", session, window))
	return cmd.Run()
}

// isValidClaudeState returns true if the state is a known ClaudeState constant.
func isValidClaudeState(s types.ClaudeState) bool {
	switch s {
	case types.ClaudeStateIdle, types.ClaudeStateRunning,
		types.ClaudeStateWaitingForInput, types.ClaudeStateStopped,
		types.ClaudeStateError, types.ClaudeStateUnknown:
		return true
	}
	return false
}

// stripEmojiPrefix removes known status emoji prefixes from a window name.
func stripEmojiPrefix(name string) string {
	prefixes := []string{"🟢", "❓", "❔", "💬", "⚫", "⚠️", "💤", "⏸️"}
	for _, p := range prefixes {
		name = strings.TrimPrefix(name, p)
	}
	return strings.TrimSpace(name)
}

// GetDetailedClaudeState returns the detailed state of Claude in a session.
// It reads the state file and computes the aggregate state from all non-stale agents.
func (m *Manager) GetDetailedClaudeState(session string, staleThreshold time.Duration) (types.ClaudeState, time.Time) {
	statusDir := status.DefaultStatusDir()

	sf, err := status.ReadStateFile(statusDir, session)
	if err != nil {
		return types.ClaudeStateUnknown, time.Time{}
	}

	return status.ComputeState(sf, staleThreshold)
}

// RenameWindow renames a window in a tmux session
func (m *Manager) RenameWindow(session, window, newName string) error {
	target := fmt.Sprintf("%s:%s", session, window)
	cmd := exec.Command("tmux", "rename-window", "-t", target, newName)
	return cmd.Run()
}

// GetSessionNameFromPane returns the session name for a given pane ID
func (m *Manager) GetSessionNameFromPane(paneID string) (string, error) {
	cmd := exec.Command("tmux", "display-message", "-t", paneID, "-p", "#{session_name}")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// RenameWindowByPane renames the window containing the given pane ID
func (m *Manager) RenameWindowByPane(paneID, newName string) error {
	cmd := exec.Command("tmux", "rename-window", "-t", paneID, newName)
	return cmd.Run()
}
