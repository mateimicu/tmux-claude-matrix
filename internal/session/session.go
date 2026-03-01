package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mateimicu/tmux-claude-matrix/pkg/types"
)

// Manager manages session metadata
type Manager struct {
	metadataDir string
}

// NewManager creates a new session manager
func NewManager(metadataDir string) *Manager {
	return &Manager{metadataDir: metadataDir}
}

// Save writes session metadata to disk
func (m *Manager) Save(s *types.Session) error {
	if err := os.MkdirAll(m.metadataDir, 0755); err != nil {
		return err
	}

	path := filepath.Join(m.metadataDir, s.Name+".json")
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// Load reads session metadata from disk.
func (m *Manager) Load(name string) (*types.Session, error) {
	path := filepath.Join(m.metadataDir, name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var s types.Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}

	return &s, nil
}

// List returns all sessions
func (m *Manager) List() ([]*types.Session, error) {
	entries, err := os.ReadDir(m.metadataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []*types.Session
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".json" {
			name := strings.TrimSuffix(entry.Name(), ".json")
			if s, err := m.Load(name); err == nil {
				sessions = append(sessions, s)
			}
		}
	}

	return sessions, nil
}

// Delete removes session metadata
func (m *Manager) Delete(name string) error {
	path := filepath.Join(m.metadataDir, name+".json")
	return os.Remove(path)
}

// Exists checks if a session exists
func (m *Manager) Exists(name string) bool {
	path := filepath.Join(m.metadataDir, name+".json")
	_, err := os.Stat(path)
	return err == nil
}

// GenerateUniqueName creates a unique session name
func (m *Manager) GenerateUniqueName(base string) (string, error) {
	name := sanitizeName(base)
	counter := 1

	for {
		if !m.Exists(name) {
			return name, nil
		}
		name = fmt.Sprintf("%s-%d", sanitizeName(base), counter)
		counter++

		if counter > 100 {
			return "", fmt.Errorf("too many sessions with base name: %s", base)
		}
	}
}

// sanitizeName converts a string to a valid tmux session name
func sanitizeName(s string) string {
	// Remove special characters, keep alphanumeric, dash, underscore
	reg := regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
	clean := reg.ReplaceAllString(s, "-")

	// Remove leading/trailing dashes
	clean = strings.Trim(clean, "-")

	// Convert to lowercase
	clean = strings.ToLower(clean)

	// Limit length
	if len(clean) > 50 {
		clean = clean[:50]
	}

	return clean
}
