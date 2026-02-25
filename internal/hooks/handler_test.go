package hooks

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mateimicu/tmux-claude-matrix/internal/status"
	"github.com/mateimicu/tmux-claude-matrix/internal/tmux"
	"github.com/mateimicu/tmux-claude-matrix/pkg/types"
)

func TestMapEventToState(t *testing.T) {
	tests := []struct {
		name  string
		event HookEvent
		want  types.ClaudeState
	}{
		{
			name:  "SessionStart maps to idle",
			event: HookEvent{HookEventName: "SessionStart"},
			want:  types.ClaudeStateIdle,
		},
		{
			name:  "UserPromptSubmit maps to running",
			event: HookEvent{HookEventName: "UserPromptSubmit"},
			want:  types.ClaudeStateRunning,
		},
		{
			name:  "PreToolUse maps to running",
			event: HookEvent{HookEventName: "PreToolUse"},
			want:  types.ClaudeStateRunning,
		},
		{
			name:  "Stop maps to idle",
			event: HookEvent{HookEventName: "Stop"},
			want:  types.ClaudeStateIdle,
		},
		{
			name:  "Notification with permission_prompt maps to waiting_for_input",
			event: HookEvent{HookEventName: "Notification", NotificationType: "permission_prompt"},
			want:  types.ClaudeStateWaitingForInput,
		},
		{
			name:  "Notification with elicitation_dialog maps to waiting_for_input",
			event: HookEvent{HookEventName: "Notification", NotificationType: "elicitation_dialog"},
			want:  types.ClaudeStateWaitingForInput,
		},
		{
			name:  "Notification with idle_prompt maps to idle",
			event: HookEvent{HookEventName: "Notification", NotificationType: "idle_prompt"},
			want:  types.ClaudeStateIdle,
		},
		{
			name:  "SessionEnd maps to stopped",
			event: HookEvent{HookEventName: "SessionEnd"},
			want:  types.ClaudeStateStopped,
		},
		{
			name:  "unknown event maps to unknown",
			event: HookEvent{HookEventName: "SomethingElse"},
			want:  types.ClaudeStateUnknown,
		},
		{
			name:  "Notification with unknown type maps to unknown",
			event: HookEvent{HookEventName: "Notification", NotificationType: "something_new"},
			want:  types.ClaudeStateUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapEventToState(&tt.event)
			if got != tt.want {
				t.Errorf("MapEventToState(%+v) = %q, want %q", tt.event, got, tt.want)
			}
		})
	}
}

func TestParseHookEvent(t *testing.T) {
	event := HookEvent{
		HookEventName:    "Notification",
		NotificationType: "permission_prompt",
		SessionID:        "sess-abc-123",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal test event: %v", err)
	}

	var parsed HookEvent
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&parsed); err != nil {
		t.Fatalf("failed to decode hook event: %v", err)
	}

	if parsed.HookEventName != "Notification" {
		t.Errorf("HookEventName = %q, want %q", parsed.HookEventName, "Notification")
	}
	if parsed.NotificationType != "permission_prompt" {
		t.Errorf("NotificationType = %q, want %q", parsed.NotificationType, "permission_prompt")
	}
	if parsed.SessionID != "sess-abc-123" {
		t.Errorf("SessionID = %q, want %q", parsed.SessionID, "sess-abc-123")
	}
}

func TestHandleHookEvent_TMUXPaneMissing(t *testing.T) {
	t.Setenv("TMUX_PANE", "")

	event := HookEvent{
		HookEventName: "UserPromptSubmit",
		SessionID:     "sess-1",
	}
	data, _ := json.Marshal(event)

	err := HandleHookEvent(bytes.NewReader(data), tmux.New(), 15*time.Minute, nil)
	if err == nil {
		t.Fatal("expected error when TMUX_PANE is empty")
	}
	if !strings.Contains(err.Error(), "TMUX_PANE") {
		t.Errorf("error should mention TMUX_PANE, got: %v", err)
	}
}

func TestHandleHookEvent_InvalidJSON(t *testing.T) {
	t.Setenv("TMUX_PANE", "%123")

	err := HandleHookEvent(bytes.NewReader([]byte("not json")), tmux.New(), 15*time.Minute, nil)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestHandleHookEvent_PerAgentWrite(t *testing.T) {
	// This test verifies that HandleHookEvent writes per-agent state
	// We can't easily test the full flow (needs tmux), but we can test
	// that the TMUX_PANE check works properly by verifying the error path
	t.Setenv("TMUX_PANE", "")

	event := HookEvent{
		HookEventName: "SessionStart",
		SessionID:     "sess-1",
	}
	data, _ := json.Marshal(event)

	err := HandleHookEvent(bytes.NewReader(data), tmux.New(), 15*time.Minute, nil)
	if err == nil {
		t.Fatal("expected error when TMUX_PANE is empty")
	}
}

// TestPerAgentStateIntegration tests the per-agent state tracking
// at the status package level (unit test for the handler's core logic)
func TestPerAgentStateIntegration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "handler-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	sessionName := "test-sess"

	// Agent 1 fires UserPromptSubmit (running)
	if err := status.UpdateAgentState(tmpDir, sessionName, "sess-1", types.ClaudeStateRunning); err != nil {
		t.Fatal(err)
	}

	// Agent 2 fires SessionStart (idle)
	if err := status.UpdateAgentState(tmpDir, sessionName, "sess-2", types.ClaudeStateIdle); err != nil {
		t.Fatal(err)
	}

	// Read and compute
	sf, err := status.ReadStateFile(tmpDir, sessionName)
	if err != nil {
		t.Fatal(err)
	}

	aggregate, _ := status.ComputeState(sf, 15*time.Minute)
	if aggregate != types.ClaudeStateRunning {
		t.Errorf("aggregate = %q, want running", aggregate)
	}
}

func TestPerAgentStateIntegration_IdempotencyRemoved(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "handler-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	sessionName := "test-sess"

	// Agent 1 fires running
	if err := status.UpdateAgentState(tmpDir, sessionName, "sess-1", types.ClaudeStateRunning); err != nil {
		t.Fatal(err)
	}

	sf1, _ := status.ReadStateFile(tmpDir, sessionName)
	ts1 := sf1.Agents["sess-1"].UpdatedAt

	// Small delay to ensure timestamp differs
	time.Sleep(10 * time.Millisecond)

	// Same agent fires running again — should still update timestamp
	if err := status.UpdateAgentState(tmpDir, sessionName, "sess-1", types.ClaudeStateRunning); err != nil {
		t.Fatal(err)
	}

	sf2, _ := status.ReadStateFile(tmpDir, sessionName)
	ts2 := sf2.Agents["sess-1"].UpdatedAt

	if !ts2.After(ts1) {
		t.Error("timestamp should be updated even when state didn't change (idempotency removed)")
	}
}

func TestPerAgentStateIntegration_SessionEnd(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "handler-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	sessionName := "test-sess"

	// Two agents
	if err := status.UpdateAgentState(tmpDir, sessionName, "sess-1", types.ClaudeStateRunning); err != nil {
		t.Fatal(err)
	}
	if err := status.UpdateAgentState(tmpDir, sessionName, "sess-2", types.ClaudeStateIdle); err != nil {
		t.Fatal(err)
	}

	// Agent 2 fires SessionEnd
	if err := status.RemoveAgentState(tmpDir, sessionName, "sess-2"); err != nil {
		t.Fatal(err)
	}

	// Agent 1 should remain
	sf, err := status.ReadStateFile(tmpDir, sessionName)
	if err != nil {
		t.Fatal(err)
	}

	if len(sf.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(sf.Agents))
	}

	aggregate, _ := status.ComputeState(sf, 15*time.Minute)
	if aggregate != types.ClaudeStateRunning {
		t.Errorf("aggregate = %q, want running", aggregate)
	}
}

func TestPerAgentStateIntegration_LastAgentSessionEnd(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "handler-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	sessionName := "test-sess"

	// One agent
	if err := status.UpdateAgentState(tmpDir, sessionName, "sess-1", types.ClaudeStateRunning); err != nil {
		t.Fatal(err)
	}

	// Agent fires SessionEnd
	if err := status.RemoveAgentState(tmpDir, sessionName, "sess-1"); err != nil {
		t.Fatal(err)
	}

	// File should be gone
	_, err = status.ReadStateFile(tmpDir, sessionName)
	if err == nil {
		t.Fatal("expected file to be removed after last agent's SessionEnd")
	}
}
