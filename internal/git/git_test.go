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

func TestAddWorktree(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a base repo with at least one commit
	baseRepo := filepath.Join(tmpDir, "base")
	initTestRepo(t, baseRepo)
	addTestCommit(t, baseRepo)

	worktreePath := filepath.Join(tmpDir, "worktrees", "test-wt")
	m := New()

	if err := m.AddWorktree(baseRepo, worktreePath, "worktree-test"); err != nil {
		t.Fatalf("AddWorktree() error = %v", err)
	}

	// Worktree directory should exist and be a git checkout
	if !isGitRepo(worktreePath) {
		t.Error("expected worktree to be a git repo")
	}
}

func TestRemoveWorktree(t *testing.T) {
	tmpDir := t.TempDir()

	baseRepo := filepath.Join(tmpDir, "base")
	initTestRepo(t, baseRepo)
	addTestCommit(t, baseRepo)

	worktreePath := filepath.Join(tmpDir, "worktrees", "test-wt")
	m := New()

	if err := m.AddWorktree(baseRepo, worktreePath, "worktree-rm-test"); err != nil {
		t.Fatalf("AddWorktree() error = %v", err)
	}

	if err := m.RemoveWorktree(baseRepo, worktreePath); err != nil {
		t.Fatalf("RemoveWorktree() error = %v", err)
	}

	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Error("expected worktree directory to be removed")
	}
}

func TestListWorktrees(t *testing.T) {
	tmpDir := t.TempDir()

	baseRepo := filepath.Join(tmpDir, "base")
	initTestRepo(t, baseRepo)
	addTestCommit(t, baseRepo)

	m := New()

	// Add two worktrees
	wt1 := filepath.Join(tmpDir, "worktrees", "wt1")
	wt2 := filepath.Join(tmpDir, "worktrees", "wt2")
	if err := m.AddWorktree(baseRepo, wt1, "branch-1"); err != nil {
		t.Fatalf("AddWorktree(wt1) error = %v", err)
	}
	if err := m.AddWorktree(baseRepo, wt2, "branch-2"); err != nil {
		t.Fatalf("AddWorktree(wt2) error = %v", err)
	}

	worktrees, err := m.ListWorktrees(baseRepo)
	if err != nil {
		t.Fatalf("ListWorktrees() error = %v", err)
	}

	// Should have 3: the base repo + 2 worktrees
	if len(worktrees) != 3 {
		t.Errorf("expected 3 worktrees, got %d", len(worktrees))
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

// addTestCommit creates an empty commit in the repo.
func addTestCommit(t *testing.T, repoPath string) {
	t.Helper()
	cmd := exec.Command("git", "-C", repoPath, "commit", "--allow-empty", "--no-gpg-sign", "-m", "test commit")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v\n%s", err, output)
	}
}
