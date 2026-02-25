package hooks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const hookMarker = "--from=tmux-claude-matrix"

// hookEventDefs defines the hook events we register, with optional matchers.
var hookEventDefs = []struct {
	event   string
	matcher string
}{
	{event: "UserPromptSubmit"},
	{event: "PreToolUse"},
	{event: "Stop"},
	{event: "Notification"},
	{event: "SessionStart", matcher: "startup"},
	{event: "SessionEnd"},
}

// SettingsPath returns the default path to Claude's settings.json.
func SettingsPath() string {
	return filepath.Join(os.Getenv("HOME"), ".claude/settings.json")
}

// SetupHooks adds our hook entries to the Claude settings file.
func SetupHooks(binaryPath string) error {
	return setupHooksToFile(binaryPath, SettingsPath())
}

// RemoveHooks removes our hook entries from the Claude settings file.
func RemoveHooks() error {
	return removeHooksFromFile(SettingsPath())
}

// IsSetup checks whether our hook entries are present in the settings file.
func IsSetup(binaryPath string) (bool, error) {
	return isSetupInFile(binaryPath, SettingsPath())
}

// setupHooksToFile adds our hook entries to the given settings file path.
// If an old entry with a different binary path exists, it is replaced.
func setupHooksToFile(binaryPath, settingsPath string) error {
	settings, err := readSettingsFile(settingsPath)
	if err != nil {
		return err
	}

	hooks := ensureHooksMap(settings)
	command := binaryPath + " hook-handler " + hookMarker

	for _, def := range hookEventDefs {
		entries := getEventEntries(hooks, def.event)
		// Remove any existing entry from us (handles binary path changes)
		entries = filterOutOurEntries(entries)
		entry := buildHookEntry(command, def.matcher)
		entries = append(entries, entry)
		hooks[def.event] = entries
	}

	settings["hooks"] = hooks
	return writeSettingsFile(settingsPath, settings)
}

// removeHooksFromFile removes our hook entries from the given settings file path.
func removeHooksFromFile(settingsPath string) error {
	settings, err := readSettingsFile(settingsPath)
	if err != nil {
		return err
	}

	hooksRaw, ok := settings["hooks"]
	if !ok {
		return nil
	}
	hooks, ok := hooksRaw.(map[string]interface{})
	if !ok {
		return nil
	}

	for event := range hooks {
		entries := getEventEntries(hooks, event)
		filtered := filterOutOurEntries(entries)

		if len(filtered) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = filtered
		}
	}

	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}

	return writeSettingsFile(settingsPath, settings)
}

// isSetupInFile checks if our hooks are present for ALL events in the given settings file.
func isSetupInFile(binaryPath, settingsPath string) (bool, error) {
	missing, err := missingHookEventsInFile(binaryPath, settingsPath)
	if err != nil {
		return false, err
	}
	return len(missing) == 0, nil
}

// MissingHookEvents returns the list of event names not registered in the settings file.
// Returns nil if all events are registered.
func MissingHookEvents(binaryPath string) ([]string, error) {
	return missingHookEventsInFile(binaryPath, SettingsPath())
}

// missingHookEventsInFile returns the list of event names not registered in the given settings file.
func missingHookEventsInFile(binaryPath, settingsPath string) ([]string, error) {
	settings, err := readSettingsFile(settingsPath)
	if err != nil {
		return nil, err
	}

	hooksRaw, ok := settings["hooks"]
	if !ok {
		// No hooks at all — all events are missing
		missing := make([]string, len(hookEventDefs))
		for i, def := range hookEventDefs {
			missing[i] = def.event
		}
		return missing, nil
	}
	hooks, ok := hooksRaw.(map[string]interface{})
	if !ok {
		missing := make([]string, len(hookEventDefs))
		for i, def := range hookEventDefs {
			missing[i] = def.event
		}
		return missing, nil
	}

	command := binaryPath + " hook-handler " + hookMarker
	var missing []string
	for _, def := range hookEventDefs {
		entries := getEventEntries(hooks, def.event)
		if !hasOurHook(entries, command) {
			missing = append(missing, def.event)
		}
	}

	return missing, nil
}

// readSettingsFile reads and parses the settings JSON file.
// Returns an empty map if the file doesn't exist.
func readSettingsFile(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return make(map[string]interface{}), nil
		}
		return nil, err
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	return settings, nil
}

// writeSettingsFile atomically writes the settings map as indented JSON.
func writeSettingsFile(path string, settings map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// ensureHooksMap ensures the "hooks" key exists and is a map.
func ensureHooksMap(settings map[string]interface{}) map[string]interface{} {
	if hooksRaw, ok := settings["hooks"]; ok {
		if hooks, ok := hooksRaw.(map[string]interface{}); ok {
			return hooks
		}
	}
	hooks := make(map[string]interface{})
	settings["hooks"] = hooks
	return hooks
}

// getEventEntries extracts the entries array for an event, or returns empty slice.
func getEventEntries(hooks map[string]interface{}, event string) []interface{} {
	raw, ok := hooks[event]
	if !ok {
		return nil
	}
	entries, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	return entries
}

// buildHookEntry creates a hook entry with our command and optional matcher.
func buildHookEntry(command, matcher string) map[string]interface{} {
	entry := map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": command,
			},
		},
	}
	if matcher != "" {
		entry["matcher"] = matcher
	}
	return entry
}

// hasOurHook checks if any entry in the list contains our hook command.
func hasOurHook(entries []interface{}, command string) bool {
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
			cmd, ok := hookMap["command"].(string)
			if ok && cmd == command {
				return true
			}
		}
	}
	return false
}

// containsMarker checks if a command string contains our hook marker.
func containsMarker(cmd string) bool {
	return strings.Contains(cmd, hookMarker)
}

// filterOutOurEntries removes entries whose hooks contain our marker.
func filterOutOurEntries(entries []interface{}) []interface{} {
	var result []interface{}
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			result = append(result, entry)
			continue
		}
		if !entryContainsOurHook(entryMap) {
			result = append(result, entry)
		}
	}
	return result
}

// entryContainsOurHook checks if a hook entry has any command containing our marker.
func entryContainsOurHook(entry map[string]interface{}) bool {
	entryHooks, ok := entry["hooks"].([]interface{})
	if !ok {
		return false
	}
	for _, h := range entryHooks {
		hookMap, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		cmd, ok := hookMap["command"].(string)
		if ok && containsMarker(cmd) {
			return true
		}
	}
	return false
}
