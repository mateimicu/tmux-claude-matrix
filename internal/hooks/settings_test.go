package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSetupHooks_EmptySettings(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hooks-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	settingsPath := filepath.Join(tmpDir, "settings.json")

	if err := setupHooksToFile("/usr/local/bin/claude-matrix", settingsPath); err != nil {
		t.Fatalf("setupHooksToFile failed: %v", err)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("failed to read settings file: %v", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("invalid JSON in settings file: %v", err)
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'hooks' key in settings")
	}

	expectedEvents := []string{
		"UserPromptSubmit", "PreToolUse", "Stop",
		"Notification", "SessionStart", "SessionEnd",
	}
	for _, event := range expectedEvents {
		if _, ok := hooks[event]; !ok {
			t.Errorf("expected event %q in hooks", event)
		}
	}

	// Verify the command contains our binary path, hook-handler, and marker flag
	verifyHookCommand(t, hooks, "UserPromptSubmit", "/usr/local/bin/claude-matrix hook-handler --from=tmux-claude-matrix")

	// Verify SessionStart has the "startup" matcher
	sessionStartEntries, ok := hooks["SessionStart"].([]interface{})
	if !ok || len(sessionStartEntries) == 0 {
		t.Fatal("expected SessionStart entries")
	}
	ssEntry, ok := sessionStartEntries[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected SessionStart entry to be a map")
	}
	if ssEntry["matcher"] != "startup" {
		t.Errorf("expected SessionStart matcher to be %q, got %v", "startup", ssEntry["matcher"])
	}
}

func TestSetupHooks_ExistingHooks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hooks-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	settingsPath := filepath.Join(tmpDir, "settings.json")

	// Write existing settings with a user hook
	existing := map[string]interface{}{
		"hooks": map[string]interface{}{
			"UserPromptSubmit": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "echo user-hook",
						},
					},
				},
			},
		},
		"someOtherSetting": "value",
	}
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := setupHooksToFile("/usr/local/bin/claude-matrix", settingsPath); err != nil {
		t.Fatalf("setupHooksToFile failed: %v", err)
	}

	data, err = os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}

	// Other settings should be preserved
	if settings["someOtherSetting"] != "value" {
		t.Error("existing settings were not preserved")
	}

	hooks := settings["hooks"].(map[string]interface{})
	entries := hooks["UserPromptSubmit"].([]interface{})

	// Should have user hook + our hook
	if len(entries) < 2 {
		t.Errorf("expected at least 2 entries for UserPromptSubmit, got %d", len(entries))
	}

	// Verify user hook is still there
	firstEntry := entries[0].(map[string]interface{})
	firstHooks := firstEntry["hooks"].([]interface{})
	firstHook := firstHooks[0].(map[string]interface{})
	if firstHook["command"] != "echo user-hook" {
		t.Error("existing user hook was not preserved")
	}
}

func TestSetupHooks_Idempotent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hooks-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	settingsPath := filepath.Join(tmpDir, "settings.json")

	// Run setup twice
	if err := setupHooksToFile("/usr/local/bin/claude-matrix", settingsPath); err != nil {
		t.Fatalf("first setupHooksToFile failed: %v", err)
	}

	if err := setupHooksToFile("/usr/local/bin/claude-matrix", settingsPath); err != nil {
		t.Fatalf("second setupHooksToFile failed: %v", err)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}

	hooks := settings["hooks"].(map[string]interface{})

	// Each event should have exactly one entry (ours)
	for _, event := range []string{"UserPromptSubmit", "PreToolUse", "Stop", "Notification", "SessionStart", "SessionEnd"} {
		entries := hooks[event].([]interface{})
		count := 0
		for _, entry := range entries {
			entryMap := entry.(map[string]interface{})
			entryHooks := entryMap["hooks"].([]interface{})
			for _, h := range entryHooks {
				hookMap := h.(map[string]interface{})
				cmd, _ := hookMap["command"].(string)
				if containsMarker(cmd) {
					count++
				}
			}
		}
		if count != 1 {
			t.Errorf("event %q: expected exactly 1 of our hook entries, got %d", event, count)
		}
	}
}

func TestRemoveHooks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hooks-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	settingsPath := filepath.Join(tmpDir, "settings.json")

	// Setup hooks first, with an existing user hook
	existing := map[string]interface{}{
		"hooks": map[string]interface{}{
			"UserPromptSubmit": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "echo user-hook",
						},
					},
				},
			},
		},
	}
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Add our hooks
	if err := setupHooksToFile("/usr/local/bin/claude-matrix", settingsPath); err != nil {
		t.Fatal(err)
	}

	// Remove our hooks
	if err := removeHooksFromFile(settingsPath); err != nil {
		t.Fatalf("removeHooksFromFile failed: %v", err)
	}

	data, err = os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("hooks key should still exist (user hooks remain)")
	}

	// UserPromptSubmit should still have the user hook
	entries, ok := hooks["UserPromptSubmit"].([]interface{})
	if !ok || len(entries) != 1 {
		t.Fatalf("expected 1 user hook entry for UserPromptSubmit, got %v", entries)
	}

	// Events that only had our hooks should be removed
	for _, event := range []string{"PreToolUse", "Stop", "Notification", "SessionStart", "SessionEnd"} {
		if _, exists := hooks[event]; exists {
			t.Errorf("event %q should have been removed (no user hooks)", event)
		}
	}
}

func TestRemoveHooks_NoOurHooks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hooks-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	settingsPath := filepath.Join(tmpDir, "settings.json")

	// Write settings without our hooks
	existing := map[string]interface{}{
		"hooks": map[string]interface{}{
			"UserPromptSubmit": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "echo user-hook",
						},
					},
				},
			},
		},
	}
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Remove should be a no-op
	if err := removeHooksFromFile(settingsPath); err != nil {
		t.Fatalf("removeHooksFromFile failed: %v", err)
	}

	data, err = os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}

	hooks := settings["hooks"].(map[string]interface{})
	entries := hooks["UserPromptSubmit"].([]interface{})
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestIsSetupInFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hooks-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	t.Run("hooks present with correct binary", func(t *testing.T) {
		settingsPath := filepath.Join(tmpDir, "correct-binary.json")
		if err := setupHooksToFile("/usr/local/bin/claude-matrix", settingsPath); err != nil {
			t.Fatal(err)
		}

		ok, err := isSetupInFile("/usr/local/bin/claude-matrix", settingsPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Error("expected isSetupInFile to return true for correct binary path")
		}
	})

	t.Run("hooks present with different binary", func(t *testing.T) {
		settingsPath := filepath.Join(tmpDir, "different-binary.json")
		if err := setupHooksToFile("/usr/local/bin/claude-matrix", settingsPath); err != nil {
			t.Fatal(err)
		}

		ok, err := isSetupInFile("/other/path/claude-matrix", settingsPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Error("expected isSetupInFile to return false for different binary path")
		}
	})

	t.Run("no hooks at all", func(t *testing.T) {
		settingsPath := filepath.Join(tmpDir, "no-hooks.json")
		if err := os.WriteFile(settingsPath, []byte(`{"someKey": "value"}`), 0o644); err != nil {
			t.Fatal(err)
		}

		ok, err := isSetupInFile("/usr/local/bin/claude-matrix", settingsPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Error("expected isSetupInFile to return false when no hooks exist")
		}
	})

	t.Run("file does not exist", func(t *testing.T) {
		settingsPath := filepath.Join(tmpDir, "nonexistent.json")

		ok, err := isSetupInFile("/usr/local/bin/claude-matrix", settingsPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Error("expected isSetupInFile to return false for nonexistent file")
		}
	})
}

func TestIsSetupInFile_PartialRegistration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hooks-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	settingsPath := filepath.Join(tmpDir, "partial.json")
	binaryPath := "/usr/local/bin/claude-matrix"
	command := binaryPath + " hook-handler " + hookMarker

	// Only register 3 of 6 events
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"UserPromptSubmit": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": command},
					},
				},
			},
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": command},
					},
				},
			},
			"Stop": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": command},
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	ok, err := isSetupInFile(binaryPath, settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected isSetupInFile to return false when only 3 of 6 events are registered")
	}
}

func TestMissingHookEventsInFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hooks-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	binaryPath := "/usr/local/bin/claude-matrix"

	t.Run("all registered", func(t *testing.T) {
		settingsPath := filepath.Join(tmpDir, "all.json")
		if err := setupHooksToFile(binaryPath, settingsPath); err != nil {
			t.Fatal(err)
		}

		missing, err := missingHookEventsInFile(binaryPath, settingsPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(missing) != 0 {
			t.Errorf("expected no missing events, got %v", missing)
		}
	})

	t.Run("partial registration", func(t *testing.T) {
		settingsPath := filepath.Join(tmpDir, "partial.json")
		command := binaryPath + " hook-handler " + hookMarker

		// Only 4 of 6 events registered
		settings := map[string]interface{}{
			"hooks": map[string]interface{}{
				"UserPromptSubmit": []interface{}{
					map[string]interface{}{
						"hooks": []interface{}{
							map[string]interface{}{"type": "command", "command": command},
						},
					},
				},
				"PreToolUse": []interface{}{
					map[string]interface{}{
						"hooks": []interface{}{
							map[string]interface{}{"type": "command", "command": command},
						},
					},
				},
				"Stop": []interface{}{
					map[string]interface{}{
						"hooks": []interface{}{
							map[string]interface{}{"type": "command", "command": command},
						},
					},
				},
				"Notification": []interface{}{
					map[string]interface{}{
						"hooks": []interface{}{
							map[string]interface{}{"type": "command", "command": command},
						},
					},
				},
			},
		}
		data, _ := json.MarshalIndent(settings, "", "  ")
		if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
			t.Fatal(err)
		}

		missing, err := missingHookEventsInFile(binaryPath, settingsPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(missing) != 2 {
			t.Fatalf("expected 2 missing events, got %d: %v", len(missing), missing)
		}

		// The missing events should be SessionStart and SessionEnd
		missingMap := make(map[string]bool)
		for _, m := range missing {
			missingMap[m] = true
		}
		if !missingMap["SessionStart"] {
			t.Error("expected SessionStart to be missing")
		}
		if !missingMap["SessionEnd"] {
			t.Error("expected SessionEnd to be missing")
		}
	})

	t.Run("no events registered", func(t *testing.T) {
		settingsPath := filepath.Join(tmpDir, "empty.json")
		if err := os.WriteFile(settingsPath, []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}

		missing, err := missingHookEventsInFile(binaryPath, settingsPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(missing) != 6 {
			t.Errorf("expected 6 missing events, got %d", len(missing))
		}
	})
}

// verifyHookCommand checks that a hook event has an entry with the expected command.
func verifyHookCommand(t *testing.T, hooks map[string]interface{}, event, expectedCmd string) {
	t.Helper()
	entries, ok := hooks[event].([]interface{})
	if !ok || len(entries) == 0 {
		t.Errorf("no entries for event %q", event)
		return
	}

	found := false
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		entryHooks, ok := entryMap["hooks"].([]interface{})
		if !ok {
			continue
		}
		for _, h := range entryHooks {
			hookMap, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			if hookMap["command"] == expectedCmd {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("event %q: expected hook with command %q not found", event, expectedCmd)
	}
}
