package types

import (
	"encoding/json"
	"time"
)

// Repository represents a discovered repository or workspace
type Repository struct {
	Source         string   `json:"source"`          // "local", "github", "workspace"
	URL            string   `json:"url"`             // Clone URL (empty for workspaces)
	Name           string   `json:"name"`            // Display name (org/repo or workspace name)
	Description    string   `json:"description"`     // Optional description
	IsWorkspace    bool     `json:"is_workspace"`    // True if this is a multi-repo workspace
	WorkspaceRepos []string `json:"workspace_repos"` // Repo URLs for workspaces
}

// Session represents a tmux session managed by matrix
type Session struct {
	CreatedAt    time.Time `json:"created_at"`
	Name         string    `json:"name"`
	Title        string    `json:"title"`
	RepoURL      string    `json:"repo_url"`
	BaseRepoPath string    `json:"base_repo_path,omitempty"`
	RepoURLs     []string  `json:"repo_urls,omitempty"` // Multiple repos for workspaces
}

// UnmarshalJSON handles backward compatibility by migrating old fields
// (clone_path, worktree_path) to BaseRepoPath when present.
func (s *Session) UnmarshalJSON(data []byte) error {
	type sessionAlias Session
	aux := &struct {
		ClonePath    string `json:"clone_path"`
		WorktreePath string `json:"worktree_path"`
		*sessionAlias
	}{
		sessionAlias: (*sessionAlias)(s),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if s.BaseRepoPath == "" {
		if aux.WorktreePath != "" {
			s.BaseRepoPath = aux.WorktreePath
		} else if aux.ClonePath != "" {
			s.BaseRepoPath = aux.ClonePath
		}
	}
	return nil
}

// ClaudeState represents the detailed state of a Claude process
type ClaudeState string

const (
	// ClaudeStateUnknown indicates state cannot be determined
	ClaudeStateUnknown ClaudeState = "unknown"
	// ClaudeStateStopped indicates Claude is not running
	ClaudeStateStopped ClaudeState = "stopped"
	// ClaudeStateRunning indicates Claude is actively processing
	ClaudeStateRunning ClaudeState = "running"
	// ClaudeStateWaitingForInput indicates Claude is waiting for user input
	ClaudeStateWaitingForInput ClaudeState = "waiting_for_input"
	// ClaudeStateIdle indicates Claude finished and is idle
	ClaudeStateIdle ClaudeState = "idle"
	// ClaudeStateError indicates Claude encountered an error
	ClaudeStateError ClaudeState = "error"
)

// SessionStatus represents runtime session information
type SessionStatus struct {
	Session       *Session
	TmuxActive    bool
	ClaudeRunning bool
	ClaudeState   ClaudeState
	LastActivity  time.Time
}

// Config represents plugin configuration
type Config struct {
	BaseRepoDir        string
	LocalReposFile     string
	WorkspacesFile     string
	ClaudeBin          string
	CacheDir           string
	SessionsDir        string
	GitHubOrgs         []string
	ClaudeArgs         []string
	CacheTTL           time.Duration
	GitHubEnabled      bool
	LocalConfigEnabled bool
	WorkspacesEnabled  bool
	Debug              bool
}
