package tmux

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/mateimicu/tmux-claude-matrix/internal/status"
	"github.com/mateimicu/tmux-claude-matrix/pkg/types"
)

func TestStripEmojiPrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"claude", "claude"},
		{"🟢claude", "claude"},
		{"❓claude", "claude"},
		{"💬claude", "claude"},
		{"⚫claude", "claude"},
		{"⚠️claude", "claude"},
		{"💤claude", "claude"},
		{"⏸️claude", "claude"},
		{"🟢 claude", "claude"},
		{"some-window", "some-window"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripEmojiPrefix(tt.input)
			if got != tt.want {
				t.Errorf("stripEmojiPrefix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGetDetailedClaudeState_MultiAgent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tmux-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Override HOME so DefaultStatusDir uses our temp dir
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome) //nolint:errcheck

	statusDir := status.DefaultStatusDir()
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write multi-agent state file
	if err := status.UpdateAgentState(statusDir, "test-sess", "agent-1", types.ClaudeStateRunning); err != nil {
		t.Fatal(err)
	}
	if err := status.UpdateAgentState(statusDir, "test-sess", "agent-2", types.ClaudeStateIdle); err != nil {
		t.Fatal(err)
	}

	m := New()
	state, ts := m.GetDetailedClaudeState("test-sess", 15*time.Minute)

	if state != types.ClaudeStateRunning {
		t.Errorf("state = %q, want running", state)
	}
	if ts.IsZero() {
		t.Error("timestamp should not be zero")
	}
}

func TestGetDetailedClaudeState_AllStale(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tmux-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	t.Setenv("HOME", tmpDir)

	statusDir := status.DefaultStatusDir()
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a state file directly with old timestamps
	sf := status.StateFile{
		Agents: map[string]status.AgentState{
			"agent-1": {State: types.ClaudeStateRunning, UpdatedAt: time.Now().Add(-20 * time.Minute)},
		},
	}
	data, _ := json.Marshal(sf)
	if err := os.WriteFile(statusDir+"/test-sess.state", data, 0o644); err != nil {
		t.Fatal(err)
	}

	m := New()
	state, _ := m.GetDetailedClaudeState("test-sess", 15*time.Minute)

	if state != types.ClaudeStateUnknown {
		t.Errorf("state = %q, want unknown (all stale)", state)
	}
}

func TestGetDetailedClaudeState_Missing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tmux-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	t.Setenv("HOME", tmpDir)

	m := New()
	state, ts := m.GetDetailedClaudeState("nonexistent", 15*time.Minute)

	if state != types.ClaudeStateUnknown {
		t.Errorf("state = %q, want unknown", state)
	}
	if !ts.IsZero() {
		t.Error("timestamp should be zero for missing state")
	}
}

func TestGetDetailedClaudeState_OldFormat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tmux-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	t.Setenv("HOME", tmpDir)

	statusDir := status.DefaultStatusDir()
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write old format
	oldFormat := map[string]interface{}{
		"state":      "running",
		"updated_at": time.Now().Format(time.RFC3339Nano),
		"session_id": "old-sess",
	}
	data, _ := json.Marshal(oldFormat)
	if err := os.WriteFile(statusDir+"/test-sess.state", data, 0o644); err != nil {
		t.Fatal(err)
	}

	m := New()
	state, _ := m.GetDetailedClaudeState("test-sess", 15*time.Minute)

	if state != types.ClaudeStateRunning {
		t.Errorf("state = %q, want running (backward compat)", state)
	}
}
