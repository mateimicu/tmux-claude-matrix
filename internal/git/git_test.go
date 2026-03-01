package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEnsureBaseRepo_ClonesNewRepo(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a source repo to clone from
	sourceRepo := filepath.Join(tmpDir, "source")
	initTestRepo(t, sourceRepo)

	baseRepoDir := filepath.Join(tmpDir, "base-repos")
	m := New()

	repoPath, err := m.EnsureBaseRepo("file://"+sourceRepo, baseRepoDir)
	if err != nil {
		t.Fatalf("EnsureBaseRepo() error = %v", err)
	}

	if !isGitRepo(repoPath) {
		t.Error("expected a git repo at the returned path")
	}
}

func TestEnsureBaseRepo_FetchesExistingRepo(t *testing.T) {
	tmpDir := t.TempDir()

	sourceRepo := filepath.Join(tmpDir, "source")
	initTestRepo(t, sourceRepo)

	baseRepoDir := filepath.Join(tmpDir, "base-repos")
	m := New()

	// First call: clone
	repoPath1, err := m.EnsureBaseRepo("file://"+sourceRepo, baseRepoDir)
	if err != nil {
		t.Fatalf("first EnsureBaseRepo() error = %v", err)
	}

	// Second call: fetch (should not error)
	repoPath2, err := m.EnsureBaseRepo("file://"+sourceRepo, baseRepoDir)
	if err != nil {
		t.Fatalf("second EnsureBaseRepo() error = %v", err)
	}

	if repoPath1 != repoPath2 {
		t.Errorf("paths should be the same: %q != %q", repoPath1, repoPath2)
	}
}

func TestGetBaseRepoPath(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		baseRepoDir string
		expected    string
	}{
		{
			name:        "HTTPS URL",
			url:         "https://github.com/org/repo.git",
			baseRepoDir: "/repos",
			expected:    "/repos/org-repo",
		},
		{
			name:        "SSH URL",
			url:         "git@github.com:org/repo",
			baseRepoDir: "/repos",
			expected:    "/repos/org-repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetBaseRepoPath(tt.url, tt.baseRepoDir)
			if result != tt.expected {
				t.Errorf("GetBaseRepoPath(%q, %q) = %q, expected %q", tt.url, tt.baseRepoDir, result, tt.expected)
			}
		})
	}
}

func TestIsGitRepo(t *testing.T) {
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "test-repo")

	if isGitRepo(repoPath) {
		t.Error("isGitRepo should return false for non-existent path")
	}

	initTestRepo(t, repoPath)

	if !isGitRepo(repoPath) {
		t.Error("isGitRepo should return true for git repo")
	}
}

func TestExtractRepoName(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "HTTPS URL",
			url:      "https://github.com/mateimicu/tmux-claude-fleet",
			expected: "mateimicu/tmux-claude-fleet",
		},
		{
			name:     "HTTPS URL with .git",
			url:      "https://github.com/mateimicu/tmux-claude-fleet.git",
			expected: "mateimicu/tmux-claude-fleet",
		},
		{
			name:     "SSH URL",
			url:      "git@github.com:mateimicu/tmux-claude-fleet.git",
			expected: "mateimicu/tmux-claude-fleet",
		},
		{
			name:     "SSH URL without .git",
			url:      "git@github.com:mateimicu/tmux-claude-fleet",
			expected: "mateimicu/tmux-claude-fleet",
		},
		{
			name:     "URL with trailing slash",
			url:      "https://github.com/mateimicu/tmux-claude-fleet/",
			expected: "mateimicu/tmux-claude-fleet",
		},
		{
			name:     "Simple path",
			url:      "/path/to/repo",
			expected: "to/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractRepoName(tt.url)
			if result != tt.expected {
				t.Errorf("ExtractRepoName(%q) = %q, expected %q", tt.url, result, tt.expected)
			}
		})
	}
}

// initTestRepo creates a new git repo at the given path.
func initTestRepo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "init", path).Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	// Configure user for commits and disable signing
	for _, kv := range [][2]string{
		{"user.email", "test@test.com"},
		{"user.name", "Test"},
		{"commit.gpgsign", "false"},
	} {
		if err := exec.Command("git", "-C", path, "config", kv[0], kv[1]).Run(); err != nil {
			t.Fatalf("git config %s failed: %v", kv[0], err)
		}
	}
}
