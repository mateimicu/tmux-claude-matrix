package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mateimicu/tmux-claude-matrix/pkg/types"
)

func TestSessionManager(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "session-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir)

	// Test Save and Load
	t.Run("SaveAndLoad", func(t *testing.T) {
		sess := &types.Session{
			Name:         "test-session",
			Title:        "test/repo #1",
			RepoURL:      "https://github.com/test/repo",
			WorktreePath: "/tmp/test",
			CreatedAt:    time.Now(),
		}

		if err := mgr.Save(sess); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		loaded, err := mgr.Load("test-session")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if loaded.Name != sess.Name {
			t.Errorf("Name mismatch: got %q, want %q", loaded.Name, sess.Name)
		}
		if loaded.Title != sess.Title {
			t.Errorf("Title mismatch: got %q, want %q", loaded.Title, sess.Title)
		}
		if loaded.RepoURL != sess.RepoURL {
			t.Errorf("RepoURL mismatch: got %q, want %q", loaded.RepoURL, sess.RepoURL)
		}
	})

	// Test List
	t.Run("List", func(t *testing.T) {
		sessions, err := mgr.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(sessions) != 1 {
			t.Errorf("Expected 1 session, got %d", len(sessions))
		}
	})

	// Test Exists
	t.Run("Exists", func(t *testing.T) {
		if !mgr.Exists("test-session") {
			t.Error("Session should exist")
		}

		if mgr.Exists("non-existent") {
			t.Error("Non-existent session should not exist")
		}
	})

	// Test Delete
	t.Run("Delete", func(t *testing.T) {
		if err := mgr.Delete("test-session"); err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		if mgr.Exists("test-session") {
			t.Error("Session should not exist after deletion")
		}
	})
}

func TestGenerateUniqueName(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir)

	// Test unique name generation
	t.Run("UniqueNames", func(t *testing.T) {
		name1, err := mgr.GenerateUniqueName("test-repo")
		if err != nil {
			t.Fatalf("GenerateUniqueName failed: %v", err)
		}

		// Create a session with that name
		sess := &types.Session{
			Name:         name1,
			RepoURL:      "https://github.com/test/repo",
			WorktreePath: "/tmp/test",
			CreatedAt:    time.Now(),
		}
		if err := mgr.Save(sess); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		// Generate another name (should be different)
		name2, err := mgr.GenerateUniqueName("test-repo")
		if err != nil {
			t.Fatalf("GenerateUniqueName failed: %v", err)
		}

		if name1 == name2 {
			t.Errorf("Names should be unique: %q == %q", name1, name2)
		}
	})
}

func TestBackwardCompatibility_MissingTitle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session-compat-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Write a session JSON in the old format (no title field)
	oldJSON := `{
  "created_at": "2025-01-01T00:00:00Z",
  "name": "old-session",
  "repo_url": "https://github.com/test/repo",
  "clone_path": "/tmp/old"
}`
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "old-session.json"), []byte(oldJSON), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(tmpDir)
	sess, err := mgr.Load("old-session")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if sess.Title != "" {
		t.Errorf("expected empty title for old session, got %q", sess.Title)
	}
	if sess.Name != "old-session" {
		t.Errorf("expected name %q, got %q", "old-session", sess.Name)
	}
	// Old clone_path should migrate to WorktreePath
	if sess.WorktreePath != "/tmp/old" {
		t.Errorf("expected WorktreePath %q from old clone_path, got %q", "/tmp/old", sess.WorktreePath)
	}
}

func TestRenameFlow(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session-rename-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir)

	// Create a session with auto-generated title
	sess := &types.Session{
		Name:         "my-session",
		Title:        "org/repo #1",
		RepoURL:      "https://github.com/org/repo",
		WorktreePath: "/tmp/test",
		CreatedAt:    time.Now(),
	}
	if err := mgr.Save(sess); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Simulate rename: load, change title, save
	loaded, err := mgr.Load("my-session")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	loaded.Title = "my custom title"
	if err := mgr.Save(loaded); err != nil {
		t.Fatalf("Save after rename failed: %v", err)
	}

	// Verify the rename persisted
	reloaded, err := mgr.Load("my-session")
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	if reloaded.Title != "my custom title" {
		t.Errorf("expected title %q, got %q", "my custom title", reloaded.Title)
	}
	// Other fields should remain unchanged
	if reloaded.RepoURL != sess.RepoURL {
		t.Errorf("RepoURL should not change: got %q, want %q", reloaded.RepoURL, sess.RepoURL)
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple name",
			input:    "test-repo",
			expected: "test-repo",
		},
		{
			name:     "With special characters",
			input:    "test@#$repo",
			expected: "test-repo",
		},
		{
			name:     "With slashes",
			input:    "org/repo",
			expected: "org-repo",
		},
		{
			name:     "With uppercase",
			input:    "Test-Repo",
			expected: "test-repo",
		},
		{
			name:     "With spaces",
			input:    "test repo",
			expected: "test-repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeName(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}
