package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Manager handles git operations
type Manager struct{}

// New creates a new git Manager
func New() *Manager {
	return &Manager{}
}

// EnsureBaseRepo ensures a base clone exists for the given URL. If the repo
// is already cloned at baseRepoDir/safeName, it fetches updates. Otherwise
// it performs a fresh clone. Returns the path to the base repo.
func (m *Manager) EnsureBaseRepo(url, baseRepoDir string) (string, error) {
	repoPath := GetBaseRepoPath(url, baseRepoDir)

	if isGitRepo(repoPath) {
		cmd := exec.Command("git", "-C", repoPath, "fetch", "--all", "--prune")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return repoPath, fmt.Errorf("failed to fetch updates for %s: %w", repoPath, err)
		}
		return repoPath, nil
	}

	if err := os.MkdirAll(filepath.Dir(repoPath), 0755); err != nil {
		return "", err
	}
	cmd := exec.Command("git", "clone", url, repoPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to clone %s: %w", url, err)
	}
	return repoPath, nil
}

// GetBaseRepoPath returns the filesystem path where a base clone is stored.
func GetBaseRepoPath(url, baseRepoDir string) string {
	repoName := ExtractRepoName(url)
	safeName := strings.ReplaceAll(repoName, "/", "-")
	return filepath.Join(baseRepoDir, safeName)
}

// isGitRepo checks if the path is an existing git repository.
func isGitRepo(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	if err != nil {
		return false
	}
	// .git can be a file (worktree) or directory (regular repo)
	return info != nil
}

// ExtractRepoName extracts org/repo from a git URL
func ExtractRepoName(url string) string {
	// Remove .git suffix
	clean := strings.TrimSuffix(url, ".git")

	// Remove trailing slash
	clean = strings.TrimSuffix(clean, "/")

	// Handle SSH URLs (git@github.com:org/repo)
	if strings.Contains(clean, ":") && strings.Contains(clean, "@") {
		parts := strings.Split(clean, ":")
		if len(parts) >= 2 {
			clean = parts[len(parts)-1]
		}
	}

	// Extract last two path components
	parts := strings.Split(clean, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}

	return filepath.Base(clean)
}
