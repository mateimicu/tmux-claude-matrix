package status

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mateimicu/tmux-claude-matrix/pkg/types"
)

// AgentState represents the state of a single agent in the multi-agent model.
type AgentState struct {
	State     types.ClaudeState `json:"state"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// StateFile represents the new multi-agent state file format.
type StateFile struct {
	Agents map[string]AgentState `json:"agents"`
}

// LegacyStateFile represents the old single-agent state file format.
type LegacyStateFile struct {
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
	SessionID string    `json:"session_id,omitempty"`
}

// DefaultStatusDir returns the default directory for state files.
func DefaultStatusDir() string {
	return filepath.Join(os.Getenv("HOME"), ".tmux-claude-matrix/status")
}

// WriteState atomically writes a state file for the given session (legacy format).
func WriteState(statusDir, sessionName string, state types.ClaudeState, claudeSessionID string) error {
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		return err
	}

	sf := LegacyStateFile{
		State:     string(state),
		UpdatedAt: time.Now(),
		SessionID: claudeSessionID,
	}

	data, err := json.Marshal(sf)
	if err != nil {
		return err
	}

	target := stateFilePath(statusDir, sessionName)

	tmpFile, err := os.CreateTemp(statusDir, sessionName+"*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()    //nolint:errcheck // Best-effort cleanup on write failure
		os.Remove(tmpPath) //nolint:errcheck // Best-effort cleanup on write failure
		return err
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath) //nolint:errcheck // Best-effort cleanup on close failure
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		os.Remove(tmpPath) //nolint:errcheck // Best-effort cleanup on rename failure
		return err
	}
	return nil
}

// ReadState reads and parses the state file in legacy format.
func ReadState(statusDir, sessionName string) (*LegacyStateFile, error) {
	data, err := os.ReadFile(stateFilePath(statusDir, sessionName))
	if err != nil {
		return nil, err
	}

	var sf LegacyStateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, err
	}

	return &sf, nil
}

// RemoveState deletes the state file for the given session. Returns nil if the file doesn't exist.
func RemoveState(statusDir, sessionName string) error {
	err := os.Remove(stateFilePath(statusDir, sessionName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// EmojiForState maps a ClaudeState to a display emoji.
func EmojiForState(state types.ClaudeState) string {
	switch state {
	case types.ClaudeStateRunning:
		return "\U0001f7e2" // green circle
	case types.ClaudeStateWaitingForInput:
		return "\u2753" // question mark
	case types.ClaudeStateIdle:
		return "\U0001f4ac" // speech balloon
	case types.ClaudeStateStopped:
		return "\u26ab" // black circle
	case types.ClaudeStateError:
		return "\u26a0\ufe0f" // warning sign
	case types.ClaudeStateUnknown:
		return "\u2754" // white question mark (unknown)
	default:
		return "\u2754" // white question mark (unknown)
	}
}

func stateFilePath(statusDir, sessionName string) string {
	return filepath.Join(statusDir, sessionName+".state")
}

// ReadStateFile reads and parses the state file, handling both old and new formats.
func ReadStateFile(statusDir, sessionName string) (*StateFile, error) {
	data, err := os.ReadFile(stateFilePath(statusDir, sessionName))
	if err != nil {
		return nil, err
	}

	return parseStateData(data)
}

// parseStateData parses raw JSON data into a StateFile, handling both formats.
func parseStateData(data []byte) (*StateFile, error) {
	// Try new format first
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	if _, ok := raw["agents"]; ok {
		var sf StateFile
		if err := json.Unmarshal(data, &sf); err != nil {
			return nil, err
		}
		return &sf, nil
	}

	// Old format: has "state" at top level
	var legacy LegacyStateFile
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, err
	}

	agentKey := legacy.SessionID
	if agentKey == "" {
		agentKey = "legacy"
	}

	return &StateFile{
		Agents: map[string]AgentState{
			agentKey: {
				State:     types.ClaudeState(legacy.State),
				UpdatedAt: legacy.UpdatedAt,
			},
		},
	}, nil
}

// UpdateAgentState performs a locked read-modify-write on the state file,
// updating a single agent's entry.
func UpdateAgentState(statusDir, sessionName, agentID string, state types.ClaudeState) error {
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		return err
	}

	return withFileLock(statusDir, sessionName, func() error {
		sf, err := readOrCreateStateFile(statusDir, sessionName)
		if err != nil {
			return err
		}

		sf.Agents[agentID] = AgentState{
			State:     state,
			UpdatedAt: time.Now(),
		}

		return writeStateFile(statusDir, sessionName, sf)
	})
}

// RemoveAgentState performs a locked read-modify-write on the state file,
// removing a single agent's entry. Deletes the file if the map becomes empty.
func RemoveAgentState(statusDir, sessionName, agentID string) error {
	return withFileLock(statusDir, sessionName, func() error {
		sf, err := ReadStateFile(statusDir, sessionName)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}

		delete(sf.Agents, agentID)

		if len(sf.Agents) == 0 {
			return RemoveState(statusDir, sessionName)
		}

		return writeStateFile(statusDir, sessionName, sf)
	})
}

// ComputeState iterates agent entries, excludes stale ones, and returns
// the highest-priority state and the most recent updated_at among non-stale entries.
func ComputeState(sf *StateFile, staleThreshold time.Duration) (types.ClaudeState, time.Time) {
	if len(sf.Agents) == 0 {
		return types.ClaudeStateStopped, time.Time{}
	}

	var (
		bestState    types.ClaudeState = types.ClaudeStateUnknown
		bestPriority int               = -1
		mostRecent   time.Time
		hasNonStale  bool
	)

	for _, agent := range sf.Agents {
		if time.Since(agent.UpdatedAt) > staleThreshold {
			continue
		}
		hasNonStale = true

		p := statePriority(agent.State)
		if p > bestPriority {
			bestPriority = p
			bestState = agent.State
		}

		if agent.UpdatedAt.After(mostRecent) {
			mostRecent = agent.UpdatedAt
		}
	}

	if !hasNonStale {
		return types.ClaudeStateUnknown, time.Time{}
	}

	return bestState, mostRecent
}

// statePriority returns the aggregation priority for a state.
// Higher priority wins in aggregation.
func statePriority(state types.ClaudeState) int {
	switch state {
	case types.ClaudeStateRunning:
		return 6
	case types.ClaudeStateWaitingForInput:
		return 5
	case types.ClaudeStateIdle:
		return 4
	case types.ClaudeStateError:
		return 3
	case types.ClaudeStateStopped:
		return 2
	case types.ClaudeStateUnknown:
		return 1
	default:
		return 0
	}
}

// readOrCreateStateFile reads an existing state file or returns a new empty one.
func readOrCreateStateFile(statusDir, sessionName string) (*StateFile, error) {
	sf, err := ReadStateFile(statusDir, sessionName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &StateFile{Agents: make(map[string]AgentState)}, nil
		}
		return nil, err
	}
	return sf, nil
}

// writeStateFile atomically writes the state file in the new multi-agent format.
func writeStateFile(statusDir, sessionName string, sf *StateFile) error {
	data, err := json.Marshal(sf)
	if err != nil {
		return err
	}

	target := stateFilePath(statusDir, sessionName)

	tmpFile, err := os.CreateTemp(statusDir, sessionName+"*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()    //nolint:errcheck
		os.Remove(tmpPath) //nolint:errcheck
		return err
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath) //nolint:errcheck
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		os.Remove(tmpPath) //nolint:errcheck
		return err
	}
	return nil
}

// withFileLock acquires an advisory file lock for the state file, executes fn,
// then releases the lock.
func withFileLock(statusDir, sessionName string, fn func() error) error {
	lockPath := stateFilePath(statusDir, sessionName) + ".lock"

	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		// If locking fails, proceed without lock (rare race is acceptable)
		return fn()
	}
	defer lockFile.Close()
	defer os.Remove(lockPath) //nolint:errcheck

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		// If flock fails, proceed without lock
		return fn()
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN) //nolint:errcheck

	return fn()
}
