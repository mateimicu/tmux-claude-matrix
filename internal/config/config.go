package config

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mateimicu/tmux-claude-matrix/pkg/types"
)

// Load reads config from multiple sources (env > files > defaults)
func Load() (*types.Config, error) {
	cfg := defaults()

	// Try config file locations
	paths := []string{
		filepath.Join(os.Getenv("HOME"), ".config/tmux-claude-matrix/config"),
		filepath.Join(os.Getenv("HOME"), ".tmux-claude-matrix/config"),
	}

	for _, path := range paths {
		if err := loadFromFile(cfg, path); err == nil {
			break // First found wins
		}
	}

	// Apply environment variable overrides
	applyEnvOverrides(cfg)

	// Validate
	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func defaults() *types.Config {
	home := os.Getenv("HOME")
	return &types.Config{
		BaseRepoDir:        filepath.Join(home, ".tmux-claude-matrix/repos"),
		WorktreeDir:        filepath.Join(home, ".tmux-claude-matrix/worktrees"),
		GitHubEnabled:      true,
		GitHubOrgs:         []string{}, // Empty = all orgs
		LocalConfigEnabled: true,
		LocalReposFile:     filepath.Join(home, ".tmux-claude-matrix/repos.txt"),
		WorkspacesEnabled:  true,
		WorkspacesFile:     filepath.Join(home, ".tmux-claude-matrix/workspaces.yaml"),
		ClaudeBin:          findClaudeBin(),
		ClaudeArgs:         []string{"--dangerously-skip-permissions"},
		CacheDir:           filepath.Join(home, ".tmux-claude-matrix/.cache"),
		CacheTTL:           24 * time.Hour,
		SessionsDir:        filepath.Join(home, ".tmux-claude-matrix/sessions"),
	}
}

func findClaudeBin() string {
	// Try common locations
	paths := []string{
		"/usr/local/bin/claude",
		filepath.Join(os.Getenv("HOME"), ".local/bin/claude"),
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Check PATH
	if path, err := exec.LookPath("claude"); err == nil {
		return path
	}

	return ""
}

func loadFromFile(cfg *types.Config, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse key=value
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)

		applyConfigValue(cfg, key, value)
	}

	return scanner.Err()
}

func applyConfigValue(cfg *types.Config, key, value string) {
	switch key {
	case "BASE_REPO_DIR", "CLONE_DIR":
		cfg.BaseRepoDir = value
	case "WORKTREE_DIR":
		cfg.WorktreeDir = value
	case "GITHUB_ENABLED":
		cfg.GitHubEnabled = value == "1" || value == "true"
	case "GITHUB_ORGS":
		// Parse comma-separated list of organizations
		if value != "" {
			orgs := strings.Split(value, ",")
			cfg.GitHubOrgs = make([]string, 0, len(orgs))
			for _, org := range orgs {
				trimmed := strings.TrimSpace(org)
				if trimmed != "" {
					cfg.GitHubOrgs = append(cfg.GitHubOrgs, trimmed)
				}
			}
		}
	case "LOCAL_CONFIG_ENABLED":
		cfg.LocalConfigEnabled = value == "1" || value == "true"
	case "LOCAL_REPOS_FILE":
		cfg.LocalReposFile = value
	case "CLAUDE_BIN":
		cfg.ClaudeBin = value
	case "CLAUDE_ARGS":
		cfg.ClaudeArgs = strings.Fields(value)
	case "CACHE_DIR":
		cfg.CacheDir = value
	case "CACHE_TTL":
		if duration, err := time.ParseDuration(value); err == nil {
			cfg.CacheTTL = duration
		} else if minutes, err := strconv.Atoi(value); err == nil {
			cfg.CacheTTL = time.Duration(minutes) * time.Minute
		}
	case "SESSIONS_DIR":
		cfg.SessionsDir = value
	case "WORKSPACES_ENABLED":
		cfg.WorkspacesEnabled = value == "1" || value == "true"
	case "WORKSPACES_FILE":
		cfg.WorkspacesFile = value
	case "DEBUG":
		cfg.Debug = value == "1" || value == "true"
	}
}

func applyEnvOverrides(cfg *types.Config) {
	if val := os.Getenv("TMUX_CLAUDE_MATRIX_BASE_REPO_DIR"); val != "" {
		cfg.BaseRepoDir = val
	} else if val := os.Getenv("TMUX_CLAUDE_MATRIX_CLONE_DIR"); val != "" {
		cfg.BaseRepoDir = val
	}
	if val := os.Getenv("TMUX_CLAUDE_MATRIX_WORKTREE_DIR"); val != "" {
		cfg.WorktreeDir = val
	}
	if val := os.Getenv("TMUX_CLAUDE_MATRIX_GITHUB_ENABLED"); val != "" {
		cfg.GitHubEnabled = val == "1" || val == "true"
	}
	if val := os.Getenv("TMUX_CLAUDE_MATRIX_GITHUB_ORGS"); val != "" {
		orgs := strings.Split(val, ",")
		cfg.GitHubOrgs = make([]string, 0, len(orgs))
		for _, org := range orgs {
			trimmed := strings.TrimSpace(org)
			if trimmed != "" {
				cfg.GitHubOrgs = append(cfg.GitHubOrgs, trimmed)
			}
		}
	}
	if val := os.Getenv("TMUX_CLAUDE_MATRIX_LOCAL_CONFIG_ENABLED"); val != "" {
		cfg.LocalConfigEnabled = val == "1" || val == "true"
	}
	if val := os.Getenv("TMUX_CLAUDE_MATRIX_LOCAL_REPOS_FILE"); val != "" {
		cfg.LocalReposFile = val
	}
	if val := os.Getenv("TMUX_CLAUDE_MATRIX_CLAUDE_BIN"); val != "" {
		cfg.ClaudeBin = val
	}
	if val := os.Getenv("TMUX_CLAUDE_MATRIX_CLAUDE_ARGS"); val != "" {
		cfg.ClaudeArgs = strings.Fields(val)
	}
	if val := os.Getenv("TMUX_CLAUDE_MATRIX_CACHE_DIR"); val != "" {
		cfg.CacheDir = val
	}
	if val := os.Getenv("TMUX_CLAUDE_MATRIX_CACHE_TTL"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			cfg.CacheTTL = duration
		} else if minutes, err := strconv.Atoi(val); err == nil {
			cfg.CacheTTL = time.Duration(minutes) * time.Minute
		}
	}
	if val := os.Getenv("TMUX_CLAUDE_MATRIX_SESSIONS_DIR"); val != "" {
		cfg.SessionsDir = val
	}
	if val := os.Getenv("TMUX_CLAUDE_MATRIX_WORKSPACES_ENABLED"); val != "" {
		cfg.WorkspacesEnabled = val == "1" || val == "true"
	}
	if val := os.Getenv("TMUX_CLAUDE_MATRIX_WORKSPACES_FILE"); val != "" {
		cfg.WorkspacesFile = val
	}
	if val := os.Getenv("TMUX_CLAUDE_MATRIX_DEBUG"); val != "" {
		cfg.Debug = val == "1" || val == "true"
	}
}

func validate(cfg *types.Config) error {
	if cfg.BaseRepoDir == "" {
		return fmt.Errorf("base repo directory cannot be empty")
	}
	if cfg.WorktreeDir == "" {
		return fmt.Errorf("worktree directory cannot be empty")
	}
	if cfg.SessionsDir == "" {
		return fmt.Errorf("sessions directory cannot be empty")
	}
	if cfg.CacheTTL <= 0 {
		return fmt.Errorf("cache TTL must be positive")
	}
	return nil
}
