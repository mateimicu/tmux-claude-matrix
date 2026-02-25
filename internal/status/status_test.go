package status

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mateimicu/tmux-claude-matrix/pkg/types"
)

// --- Existing tests (preserved) ---

func TestWriteAndReadState(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	sessionName := "test-session"
	state := types.ClaudeStateRunning
	claudeSessionID := "sess-abc-123"

	if err := WriteState(tmpDir, sessionName, state, claudeSessionID); err != nil {
		t.Fatalf("WriteState failed: %v", err)
	}

	sf, err := ReadState(tmpDir, sessionName)
	if err != nil {
		t.Fatalf("ReadState failed: %v", err)
	}

	if sf.State != string(state) {
		t.Errorf("State = %q, want %q", sf.State, state)
	}
	if sf.SessionID != claudeSessionID {
		t.Errorf("SessionID = %q, want %q", sf.SessionID, claudeSessionID)
	}
	if time.Since(sf.UpdatedAt) > 5*time.Second {
		t.Errorf("UpdatedAt too old: %v", sf.UpdatedAt)
	}
}

func TestReadState_NotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	_, err = ReadState(tmpDir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing state file, got nil")
	}
}

func TestRemoveState(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if err := WriteState(tmpDir, "to-remove", types.ClaudeStateIdle, ""); err != nil {
		t.Fatalf("WriteState failed: %v", err)
	}

	if err := RemoveState(tmpDir, "to-remove"); err != nil {
		t.Fatalf("RemoveState failed: %v", err)
	}

	path := filepath.Join(tmpDir, "to-remove.state")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file to be removed, stat err: %v", err)
	}

	if err := RemoveState(tmpDir, "never-existed"); err != nil {
		t.Errorf("RemoveState on non-existent file should not error, got: %v", err)
	}
}

func TestEmojiForState(t *testing.T) {
	tests := []struct {
		state types.ClaudeState
		emoji string
	}{
		{types.ClaudeStateRunning, "\U0001f7e2"},
		{types.ClaudeStateWaitingForInput, "\u2753"},
		{types.ClaudeStateIdle, "\U0001f4ac"},
		{types.ClaudeStateStopped, "\u26ab"},
		{types.ClaudeStateError, "\u26a0\ufe0f"},
		{types.ClaudeStateUnknown, "\u2754"},
		{types.ClaudeState("something-else"), "\u2754"},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			got := EmojiForState(tt.state)
			if got != tt.emoji {
				t.Errorf("EmojiForState(%q) = %q, want %q", tt.state, got, tt.emoji)
			}
		})
	}
}

func TestWriteState_AtomicCreate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	statusDir := filepath.Join(tmpDir, "nested", "status")

	if err := WriteState(statusDir, "auto-created", types.ClaudeStateRunning, ""); err != nil {
		t.Fatalf("WriteState should create directory, got: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(statusDir, "auto-created.state"))
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}

	var sf LegacyStateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatalf("state file is not valid JSON: %v", err)
	}

	if sf.State != string(types.ClaudeStateRunning) {
		t.Errorf("State = %q, want %q", sf.State, types.ClaudeStateRunning)
	}
}

// --- New multi-agent state tests ---

func TestUpdateAgentState_SingleAgent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if err := UpdateAgentState(tmpDir, "test-sess", "agent-1", types.ClaudeStateRunning); err != nil {
		t.Fatalf("UpdateAgentState failed: %v", err)
	}

	sf, err := ReadStateFile(tmpDir, "test-sess")
	if err != nil {
		t.Fatalf("ReadStateFile failed: %v", err)
	}

	if len(sf.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(sf.Agents))
	}

	agent, ok := sf.Agents["agent-1"]
	if !ok {
		t.Fatal("expected agent-1 in map")
	}
	if agent.State != types.ClaudeStateRunning {
		t.Errorf("agent-1 state = %q, want %q", agent.State, types.ClaudeStateRunning)
	}
	if time.Since(agent.UpdatedAt) > 5*time.Second {
		t.Errorf("agent-1 UpdatedAt too old: %v", agent.UpdatedAt)
	}
}

func TestUpdateAgentState_MultipleAgents(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if err := UpdateAgentState(tmpDir, "test-sess", "agent-1", types.ClaudeStateRunning); err != nil {
		t.Fatal(err)
	}
	if err := UpdateAgentState(tmpDir, "test-sess", "agent-2", types.ClaudeStateIdle); err != nil {
		t.Fatal(err)
	}

	sf, err := ReadStateFile(tmpDir, "test-sess")
	if err != nil {
		t.Fatal(err)
	}

	if len(sf.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(sf.Agents))
	}
	if sf.Agents["agent-1"].State != types.ClaudeStateRunning {
		t.Errorf("agent-1 state = %q, want running", sf.Agents["agent-1"].State)
	}
	if sf.Agents["agent-2"].State != types.ClaudeStateIdle {
		t.Errorf("agent-2 state = %q, want idle", sf.Agents["agent-2"].State)
	}
}

func TestUpdateAgentState_UpdateExisting(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if err := UpdateAgentState(tmpDir, "test-sess", "agent-1", types.ClaudeStateIdle); err != nil {
		t.Fatal(err)
	}
	if err := UpdateAgentState(tmpDir, "test-sess", "agent-1", types.ClaudeStateRunning); err != nil {
		t.Fatal(err)
	}

	sf, err := ReadStateFile(tmpDir, "test-sess")
	if err != nil {
		t.Fatal(err)
	}

	if sf.Agents["agent-1"].State != types.ClaudeStateRunning {
		t.Errorf("agent-1 state = %q, want running", sf.Agents["agent-1"].State)
	}
}

func TestRemoveAgentState_OneOfMany(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if err := UpdateAgentState(tmpDir, "test-sess", "agent-1", types.ClaudeStateRunning); err != nil {
		t.Fatal(err)
	}
	if err := UpdateAgentState(tmpDir, "test-sess", "agent-2", types.ClaudeStateIdle); err != nil {
		t.Fatal(err)
	}

	if err := RemoveAgentState(tmpDir, "test-sess", "agent-1"); err != nil {
		t.Fatalf("RemoveAgentState failed: %v", err)
	}

	sf, err := ReadStateFile(tmpDir, "test-sess")
	if err != nil {
		t.Fatal(err)
	}

	if len(sf.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(sf.Agents))
	}
	if _, ok := sf.Agents["agent-2"]; !ok {
		t.Error("expected agent-2 to remain")
	}
}

func TestRemoveAgentState_LastAgent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if err := UpdateAgentState(tmpDir, "test-sess", "agent-1", types.ClaudeStateRunning); err != nil {
		t.Fatal(err)
	}

	if err := RemoveAgentState(tmpDir, "test-sess", "agent-1"); err != nil {
		t.Fatal(err)
	}

	// File should be deleted
	path := stateFilePath(tmpDir, "test-sess")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected state file to be removed after last agent, stat err: %v", err)
	}
}

func TestRemoveAgentState_Nonexistent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if err := UpdateAgentState(tmpDir, "test-sess", "agent-1", types.ClaudeStateRunning); err != nil {
		t.Fatal(err)
	}

	if err := RemoveAgentState(tmpDir, "test-sess", "agent-99"); err != nil {
		t.Fatalf("RemoveAgentState for nonexistent agent should not error, got: %v", err)
	}

	sf, err := ReadStateFile(tmpDir, "test-sess")
	if err != nil {
		t.Fatal(err)
	}
	if len(sf.Agents) != 1 {
		t.Errorf("expected 1 agent, got %d", len(sf.Agents))
	}
}

func TestRemoveAgentState_NoFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Removing from a session with no state file should not error
	if err := RemoveAgentState(tmpDir, "nonexistent", "agent-1"); err != nil {
		t.Fatalf("RemoveAgentState for nonexistent session should not error, got: %v", err)
	}
}

func TestComputeState_RunningWins(t *testing.T) {
	sf := &StateFile{
		Agents: map[string]AgentState{
			"agent-1": {State: types.ClaudeStateRunning, UpdatedAt: time.Now()},
			"agent-2": {State: types.ClaudeStateIdle, UpdatedAt: time.Now()},
		},
	}

	state, _ := ComputeState(sf, 15*time.Minute)
	if state != types.ClaudeStateRunning {
		t.Errorf("ComputeState = %q, want running", state)
	}
}

func TestComputeState_WaitingWinsOverIdle(t *testing.T) {
	sf := &StateFile{
		Agents: map[string]AgentState{
			"agent-1": {State: types.ClaudeStateWaitingForInput, UpdatedAt: time.Now()},
			"agent-2": {State: types.ClaudeStateIdle, UpdatedAt: time.Now()},
		},
	}

	state, _ := ComputeState(sf, 15*time.Minute)
	if state != types.ClaudeStateWaitingForInput {
		t.Errorf("ComputeState = %q, want waiting_for_input", state)
	}
}

func TestComputeState_AllIdle(t *testing.T) {
	sf := &StateFile{
		Agents: map[string]AgentState{
			"agent-1": {State: types.ClaudeStateIdle, UpdatedAt: time.Now()},
			"agent-2": {State: types.ClaudeStateIdle, UpdatedAt: time.Now()},
		},
	}

	state, _ := ComputeState(sf, 15*time.Minute)
	if state != types.ClaudeStateIdle {
		t.Errorf("ComputeState = %q, want idle", state)
	}
}

func TestComputeState_StaleExcluded(t *testing.T) {
	sf := &StateFile{
		Agents: map[string]AgentState{
			"agent-1": {State: types.ClaudeStateRunning, UpdatedAt: time.Now().Add(-20 * time.Minute)},
			"agent-2": {State: types.ClaudeStateIdle, UpdatedAt: time.Now()},
		},
	}

	state, _ := ComputeState(sf, 15*time.Minute)
	if state != types.ClaudeStateIdle {
		t.Errorf("ComputeState = %q, want idle (running agent is stale)", state)
	}
}

func TestComputeState_AllStale(t *testing.T) {
	sf := &StateFile{
		Agents: map[string]AgentState{
			"agent-1": {State: types.ClaudeStateRunning, UpdatedAt: time.Now().Add(-20 * time.Minute)},
			"agent-2": {State: types.ClaudeStateIdle, UpdatedAt: time.Now().Add(-20 * time.Minute)},
		},
	}

	state, _ := ComputeState(sf, 15*time.Minute)
	if state != types.ClaudeStateUnknown {
		t.Errorf("ComputeState = %q, want unknown (all stale)", state)
	}
}

func TestComputeState_EmptyMap(t *testing.T) {
	sf := &StateFile{
		Agents: map[string]AgentState{},
	}

	state, _ := ComputeState(sf, 15*time.Minute)
	if state != types.ClaudeStateStopped {
		t.Errorf("ComputeState = %q, want stopped (empty map)", state)
	}
}

func TestComputeState_MostRecentTimestamp(t *testing.T) {
	recent := time.Now().Add(-2 * time.Second)
	older := time.Now().Add(-10 * time.Second)

	sf := &StateFile{
		Agents: map[string]AgentState{
			"agent-1": {State: types.ClaudeStateRunning, UpdatedAt: recent},
			"agent-2": {State: types.ClaudeStateIdle, UpdatedAt: older},
		},
	}

	_, ts := ComputeState(sf, 15*time.Minute)
	if !ts.Equal(recent) {
		t.Errorf("ComputeState timestamp = %v, want %v", ts, recent)
	}
}

func TestReadStateFile_BackwardCompat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Write old format
	now := time.Now().Truncate(time.Second)
	oldFormat := LegacyStateFile{
		State:     "running",
		UpdatedAt: now,
		SessionID: "sess-old-123",
	}
	data, err := json.Marshal(oldFormat)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateFilePath(tmpDir, "old-sess"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	sf, err := ReadStateFile(tmpDir, "old-sess")
	if err != nil {
		t.Fatalf("ReadStateFile failed: %v", err)
	}

	if len(sf.Agents) != 1 {
		t.Fatalf("expected 1 agent from old format, got %d", len(sf.Agents))
	}

	// Check the agent entry - key should be the old session_id
	agent, ok := sf.Agents["sess-old-123"]
	if !ok {
		t.Fatal("expected agent entry keyed by old session_id")
	}
	if agent.State != types.ClaudeStateRunning {
		t.Errorf("agent state = %q, want running", agent.State)
	}
}

func TestReadStateFile_BackwardCompatEmptySessionID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	oldFormat := LegacyStateFile{
		State:     "idle",
		UpdatedAt: time.Now(),
	}
	data, err := json.Marshal(oldFormat)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateFilePath(tmpDir, "old-sess"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	sf, err := ReadStateFile(tmpDir, "old-sess")
	if err != nil {
		t.Fatal(err)
	}

	// Should use "legacy" as sentinel key
	agent, ok := sf.Agents["legacy"]
	if !ok {
		t.Fatal("expected agent entry keyed by 'legacy' sentinel")
	}
	if agent.State != types.ClaudeStateIdle {
		t.Errorf("agent state = %q, want idle", agent.State)
	}
}

func TestReadStateFile_NewFormat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if err := UpdateAgentState(tmpDir, "test-sess", "agent-1", types.ClaudeStateRunning); err != nil {
		t.Fatal(err)
	}

	sf, err := ReadStateFile(tmpDir, "test-sess")
	if err != nil {
		t.Fatal(err)
	}

	if len(sf.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(sf.Agents))
	}
}

func TestReadStateFile_Missing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	_, err = ReadStateFile(tmpDir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing state file")
	}
}

func TestUpdateAgentState_OverwritesOldFormat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Write old format
	oldFormat := LegacyStateFile{
		State:     "running",
		UpdatedAt: time.Now(),
		SessionID: "old-sess-id",
	}
	data, err := json.Marshal(oldFormat)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateFilePath(tmpDir, "test-sess"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// New write should overwrite with multi-agent format
	if err := UpdateAgentState(tmpDir, "test-sess", "new-agent", types.ClaudeStateIdle); err != nil {
		t.Fatal(err)
	}

	sf, err := ReadStateFile(tmpDir, "test-sess")
	if err != nil {
		t.Fatal(err)
	}

	// Should have old agent converted + new agent
	if len(sf.Agents) != 2 {
		t.Fatalf("expected 2 agents (old converted + new), got %d", len(sf.Agents))
	}
	if _, ok := sf.Agents["old-sess-id"]; !ok {
		t.Error("expected old agent entry to be preserved")
	}
	if _, ok := sf.Agents["new-agent"]; !ok {
		t.Error("expected new agent entry")
	}
}

func TestUpdateAgentState_CreatesDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	statusDir := filepath.Join(tmpDir, "nested", "status")

	if err := UpdateAgentState(statusDir, "test-sess", "agent-1", types.ClaudeStateRunning); err != nil {
		t.Fatalf("UpdateAgentState should create directory, got: %v", err)
	}

	sf, err := ReadStateFile(statusDir, "test-sess")
	if err != nil {
		t.Fatal(err)
	}
	if len(sf.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(sf.Agents))
	}
}

func TestComputeState_ErrorState(t *testing.T) {
	sf := &StateFile{
		Agents: map[string]AgentState{
			"agent-1": {State: types.ClaudeStateError, UpdatedAt: time.Now()},
			"agent-2": {State: types.ClaudeStateStopped, UpdatedAt: time.Now()},
		},
	}

	state, _ := ComputeState(sf, 15*time.Minute)
	if state != types.ClaudeStateError {
		t.Errorf("ComputeState = %q, want error", state)
	}
}

func TestComputeState_StoppedOnly(t *testing.T) {
	sf := &StateFile{
		Agents: map[string]AgentState{
			"agent-1": {State: types.ClaudeStateStopped, UpdatedAt: time.Now()},
		},
	}

	state, _ := ComputeState(sf, 15*time.Minute)
	if state != types.ClaudeStateStopped {
		t.Errorf("ComputeState = %q, want stopped", state)
	}
}
