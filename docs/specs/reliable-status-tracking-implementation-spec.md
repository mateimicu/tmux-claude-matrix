# Implementation Spec: Reliable Status Tracking

**PRD:** `docs/prd/reliable-status-tracking.md`
**Branch:** `spec/reliable-status-tracking` (stacked on `prd/reliable-status-tracking`)

## Architecture Overview

This feature repairs the hook-based status tracking system by: removing unreliable process-based fallback detection, adding a session marker file for TMUX_PANE resolution, fixing the idempotency short-circuit that suppresses timestamp updates, making the staleness threshold configurable, validating all hooks are registered, adding diagnostics, and surfacing errors.

The changes touch 7 existing files and add 2 new files. No new external dependencies are required.

## Component Changes

### 1. Session Marker File (PRD Req 1)

**New file:** `internal/marker/marker.go`

This module manages session-to-pane marker files at `~/.tmux-claude-matrix/sessions/{sessionName}.env`.

**Interfaces:**

```go
package marker

// MarkerData represents the contents of a session marker file.
type MarkerData struct {
    SessionName string `json:"session_name"`
    TmuxPane    string `json:"tmux_pane"`
}

// DefaultMarkerDir returns "~/.tmux-claude-matrix/sessions".
func DefaultMarkerDir() string

// Write atomically writes a marker file mapping sessionName to tmuxPane.
func Write(markerDir, sessionName, tmuxPane string) error

// Read reads and parses the marker file for the given session.
func Read(markerDir, sessionName string) (*MarkerData, error)

// Remove deletes the marker file for the given session. Returns nil if not found.
func Remove(markerDir, sessionName string) error

// FindByPane scans all marker files in markerDir and returns the session name
// whose TmuxPane matches the given pane ID. Returns ("", nil) if no match.
func FindByPane(markerDir, tmuxPane string) (string, error)

// List returns all marker files in the directory.
func List(markerDir string) ([]*MarkerData, error)
```

**File format:** JSON, same atomic write pattern as `status.WriteState` (write to temp, rename).

**File path:** `{markerDir}/{sessionName}.env` (reuses the existing sessions directory).

**Integration points:**
- **Write:** Called from `cmd/claude-matrix/create.go` after `tmuxMgr.CreateSession()` succeeds. The pane ID is obtained by querying tmux for the session's pane (`tmux list-panes -t {session} -F "#{pane_id}"`). A new method `GetSessionPane(session string) (string, error)` is added to `tmux.Manager` for this.
- **Remove:** Called from `cmd/claude-matrix/list.go:handleDeleteAction()` alongside the existing `status.RemoveState` call.
- **Read/FindByPane:** Called from `internal/hooks/handler.go:HandleHookEvent()` as the TMUX_PANE fallback (see Section 2).

**Note on directory reuse:** The PRD specifies `~/.tmux-claude-matrix/sessions/{sessionName}.env`. The existing `session.Manager` already uses `~/.tmux-claude-matrix/sessions/` for `{sessionName}.json` metadata files. The `.env` extension avoids collision with the `.json` files. `marker.DefaultMarkerDir()` returns the same path as `cfg.SessionsDir`.

### 2. Hook Handler TMUX_PANE Fallback and Error Surfacing (PRD Reqs 1, 2, 5, 8, 9)

**Modified file:** `internal/hooks/handler.go`

**Current signature (unchanged):**
```go
func HandleHookEvent(reader io.Reader, mgr *tmux.Manager) error
```

**Behavioral changes to `HandleHookEvent`:**

1. **TMUX_PANE fallback via marker file:** When `os.Getenv("TMUX_PANE")` is empty, instead of returning `nil` silently, the handler:
   - Reads the Claude session ID from the parsed event.
   - Calls `marker.FindByPane(markerDir, "")` — but since there's no pane to match, the handler instead needs the session name. The hook event contains `session_id` (a Claude Code session ID, not a tmux session name), which is insufficient to directly locate the marker.
   - **Resolution strategy:** When TMUX_PANE is empty, scan all marker files and try each pane to resolve the session name via `mgr.GetSessionNameFromPane(pane)`. Use the first valid match. This is bounded by the number of active sessions (typically <20).
   - If no marker resolves, return an error (surfaced to stderr per Req 9).

2. **Remove idempotency short-circuit (PRD Req 2, 5):** Delete the `current.State == string(state) && current.SessionID == event.SessionID` check at handler.go:81. Always call `status.WriteState` on every event to refresh the timestamp.

3. **SessionEnd cleanup (PRD Req 8):** The existing SessionEnd handling already calls `mgr.RenameWindowByPane` and `status.RemoveState`. Add `marker.Remove(markerDir, sessionName)` to also clean up the marker file. Ensure this works when TMUX_PANE is empty by using the marker fallback from point 1.

4. **Error surfacing to stderr (PRD Req 9):** When `HandleHookEvent` returns an error, the caller (`cmd/claude-matrix/hook_handler.go`) must write it to stderr. Currently the cobra `RunE` already surfaces errors through cobra's error handling which prints to stderr. Verify this path works. Additionally, within `HandleHookEvent`, when the TMUX_PANE fallback fails, return a descriptive error rather than nil.

**Modified file:** `cmd/claude-matrix/hook_handler.go`

The existing `RunE` function returns errors which cobra prints to stderr. Add explicit `fmt.Fprintf(os.Stderr, ...)` for the error before returning to ensure it's visible even if cobra's error formatting changes.

### 3. Configurable Staleness Threshold (PRD Req 3)

**Modified file:** `pkg/types/types.go`

Add a `StaleThreshold` field to the `Config` struct:

```go
type Config struct {
    // ... existing fields ...
    StaleThreshold time.Duration
}
```

**Modified file:** `internal/config/config.go`

- In `defaults()`: set `StaleThreshold: 15 * time.Minute`
- In `applyConfigValue()`: handle `STALE_THRESHOLD` key (parse as minutes integer or duration string)
- In `applyEnvOverrides()`: handle `CLAUDE_MATRIX_STALE_THRESHOLD` env var (parse as minutes integer)

**Modified file:** `internal/tmux/tmux.go`

`GetDetailedClaudeState` currently hardcodes `5*time.Minute`. Change its signature to accept the threshold:

```go
func (m *Manager) GetDetailedClaudeState(session string, staleThreshold time.Duration) (types.ClaudeState, time.Time)
```

Callers (`cmd/claude-matrix/list.go`) pass `cfg.StaleThreshold` from the loaded config.

### 4. Remove Process-Based Fallback (PRD Req 4)

**Modified file:** `internal/tmux/tmux.go`

**Delete the following functions entirely:**
- `analyzeClaudeState` (lines 353-413)
- `capturePaneContent` (lines 286-294)
- `getProcessState` (lines 297-331)
- `getPaneLastActivity` (lines 334-351)
- `processIsClaude` (lines 126-158)
- `GetClaudeStatus` (lines 105-123) — also unused after this change

**Simplify `GetDetailedClaudeState`:**

```
func (m *Manager) GetDetailedClaudeState(session string, staleThreshold time.Duration) (types.ClaudeState, time.Time):
    1. Read state file via status.ReadState(statusDir, session)
    2. If error (file missing): return ClaudeStateUnknown, zero time
    3. If stale (status.IsStale(sf, staleThreshold)): return ClaudeStateUnknown, sf.UpdatedAt
    4. If valid state: return state, sf.UpdatedAt
    5. Otherwise: return ClaudeStateUnknown, sf.UpdatedAt
```

No process inspection. No pane content parsing. Unknown for anything outside the state file.

**Modified file:** `cmd/claude-matrix/list.go`

- Remove the call to `tmuxMgr.GetClaudeStatus(sess.Name)` — this function is deleted.
- The `ClaudeRunning` field on `SessionStatus` can be derived from `ClaudeState != Stopped && ClaudeState != Unknown`.
- Pass `cfg.StaleThreshold` to `GetDetailedClaudeState`.

**Modified file:** `internal/tmux/tmux_test.go`

- Remove `TestAnalyzeClaudeState` — the function is deleted.
- Keep `TestStripEmojiPrefix`.

### 5. Validate All Hooks Are Registered (PRD Req 5/6)

**Modified file:** `internal/hooks/settings.go`

Change `isSetupInFile` logic. Currently it returns `true` on the FIRST match. Change to return `true` only when ALL events in `hookEventDefs` have a matching entry.

**New exported function for partial registration detection:**

```go
// MissingHookEvents returns the list of hook event names that are not registered
// in the settings file. Returns nil if all are registered.
func MissingHookEvents(binaryPath string) ([]string, error)

// Internal: missingHookEventsInFile (testable variant with explicit path)
func missingHookEventsInFile(binaryPath, settingsPath string) ([]string, error)
```

**Integration point:** `cmd/claude-matrix/diagnose.go` calls `MissingHookEvents` and reports any missing events as warnings.

### 6. Debug Logging (PRD Req 6)

**New file:** `internal/debug/debug.go`

A lightweight debug logger. NOT a full logging framework — just a conditional writer.

```go
package debug

import (
    "io"
    "os"
)

// Logger writes debug messages to a log file when enabled.
type Logger struct {
    w       io.WriteCloser
    enabled bool
}

// New creates a Logger. If CLAUDE_MATRIX_DEBUG=1 or the config flag is set,
// it opens the log file at logPath for append. Otherwise, all writes are no-ops.
func New(logPath string, enabled bool) *Logger

// Log writes a timestamped message to the log file (no-op if disabled).
func (l *Logger) Log(format string, args ...interface{})

// Close closes the underlying file.
func (l *Logger) Close()

// IsEnabled returns whether debug logging is active.
func (l *Logger) IsEnabled() bool
```

**Log file path:** `~/.tmux-claude-matrix/logs/hooks.log`

**Enablement:** Check `os.Getenv("CLAUDE_MATRIX_DEBUG") == "1"` OR a config file field `DEBUG=1`. The `debug.New` function checks the env var itself; the caller can also pass `enabled=true` from config.

**Integration points:**
- `internal/hooks/handler.go`: Accept a `*debug.Logger` parameter (or create one internally). Log every event received, TMUX_PANE resolution, state transitions, and errors.
- To keep the `HandleHookEvent` signature manageable, add an `Options` struct:

```go
type HandleOptions struct {
    MarkerDir string
    Logger    *debug.Logger
}

func HandleHookEvent(reader io.Reader, mgr *tmux.Manager, opts HandleOptions) error
```

- `cmd/claude-matrix/hook_handler.go`: Create the logger and pass it via `HandleOptions`.

**Config change:** Add `Debug bool` to `types.Config`. Handle `DEBUG` key in config file and `CLAUDE_MATRIX_DEBUG` env var in `config.go`.

### 7. Diagnostic Command Enhancement (PRD Req 7)

**Modified file:** `cmd/claude-matrix/diagnose.go`

The existing `diagnose` command focuses on repository discovery. Add a new section for hook/status diagnostics. The command structure stays the same (single `diagnose` command), just with additional output sections.

**New diagnostic sections to add:**

1. **Hook registration check:**
   - Call `hooks.MissingHookEvents(binaryPath)`.
   - Report each event's registration status (registered/missing).
   - Report the binary path found in hook commands and whether it resolves to an executable (use `exec.LookPath` or `os.Stat`).

2. **State file inventory:**
   - List all `.state` files in `status.DefaultStatusDir()`.
   - For each: show session name, state value, and age (time since UpdatedAt).

3. **Marker file inventory:**
   - Call `marker.List(markerDir)`.
   - For each: show session name, pane ID, and whether the pane is still valid (query tmux).

4. **Active tmux sessions:**
   - Call `tmuxMgr.ListSessions()`.
   - For each: show session name and window names.

5. **Environment:**
   - Show `TMUX_PANE` value.
   - Show `CLAUDE_MATRIX_DEBUG` value.
   - Show `CLAUDE_MATRIX_STALE_THRESHOLD` value.
   - Show configured stale threshold from config.

**Note:** The existing repository diagnostics remain unchanged. The new sections are appended after them.

### 8. SessionEnd Marker Cleanup (PRD Req 8)

Already covered in Section 2 (Hook Handler changes). When `SessionEnd` fires:
1. Resolve pane via TMUX_PANE or marker fallback.
2. Rename window to plain "claude" (existing behavior).
3. Remove state file (existing behavior).
4. Remove marker file (new behavior).

## Data Flow Diagrams

### Session Creation Flow (with marker file)

```
User runs "claude-matrix create"
  |
  v
create.go: tmuxMgr.CreateSession(name, path, cmd)
  |
  v
create.go: pane = tmuxMgr.GetSessionPane(name)  [NEW]
  |
  v
create.go: marker.Write(sessionsDir, name, pane) [NEW]
  |
  v
create.go: sessionMgr.Save(sess)                 [existing]
```

### Hook Event Flow (with TMUX_PANE fallback)

```
Claude Code fires hook event
  |
  v
hook_handler.go: HandleHookEvent(stdin, mgr, opts)
  |
  v
Parse JSON event from stdin
  |
  v
Map event to ClaudeState
  |
  +--> TMUX_PANE set?
  |      YES: sessionName = mgr.GetSessionNameFromPane(pane)
  |      NO:  scan marker files, try each pane -> resolve sessionName [NEW]
  |           If no match: return error to stderr [NEW]
  |
  v
Always write state file (no idempotency check) [CHANGED]
  |
  +--> SessionEnd?
  |      YES: rename window "claude", remove state file, remove marker file [CHANGED]
  |      NO:  rename window "{emoji}claude"
  |
  v
Log to debug log if enabled [NEW]
```

### State Reading Flow (simplified)

```
list.go: GetDetailedClaudeState(session, staleThreshold)
  |
  v
Read state file
  |
  +--> File missing?        -> return Unknown
  +--> File stale?          -> return Unknown  [CHANGED: no fallback]
  +--> Valid state present? -> return state
```

### Session Deletion Flow (with marker cleanup)

```
list.go: handleDeleteAction()
  |
  v
Kill tmux session (existing)
  |
  v
sessionMgr.Delete(name)          [existing]
  |
  v
status.RemoveState(dir, name)    [existing]
  |
  v
marker.Remove(sessionsDir, name) [NEW]
```

## File Change Summary

| File | Change Type | PRD Reqs |
|------|-------------|----------|
| `internal/marker/marker.go` | **NEW** | 1, 8 |
| `internal/debug/debug.go` | **NEW** | 6 |
| `internal/hooks/handler.go` | MODIFY | 1, 2, 5, 6, 8, 9 |
| `internal/hooks/settings.go` | MODIFY | 5 |
| `internal/tmux/tmux.go` | MODIFY (major) | 3, 4 |
| `internal/config/config.go` | MODIFY | 3, 6 |
| `pkg/types/types.go` | MODIFY | 3, 6 |
| `cmd/claude-matrix/hook_handler.go` | MODIFY | 6, 9 |
| `cmd/claude-matrix/create.go` | MODIFY | 1 |
| `cmd/claude-matrix/list.go` | MODIFY | 1, 3, 4 |
| `cmd/claude-matrix/diagnose.go` | MODIFY (major) | 7 |

## Test Boundaries

### New test file: `internal/marker/marker_test.go`
- Write/Read/Remove round-trip
- FindByPane with multiple markers
- FindByPane with no match
- Remove non-existent file (idempotent)

### New test file: `internal/debug/debug_test.go`
- Logger enabled: writes to file
- Logger disabled: no-ops
- Log format includes timestamps

### Modified: `internal/hooks/handler_test.go`
- Test TMUX_PANE fallback: when env var is empty, handler uses marker file
- Test idempotency removal: same state+sessionID still writes (timestamp updates)
- Test SessionEnd removes marker file
- Test error is returned when TMUX_PANE is empty and no marker matches

### Modified: `internal/hooks/settings_test.go`
- Test `isSetupInFile` returns false when only some events are registered
- Test `isSetupInFile` returns true only when ALL events are registered
- Test `MissingHookEvents` returns correct missing event list

### Modified: `internal/tmux/tmux_test.go`
- Remove `TestAnalyzeClaudeState` (function deleted)
- Keep `TestStripEmojiPrefix`
- Add test for simplified `GetDetailedClaudeState`: state file present/missing/stale scenarios

### Modified: `internal/status/status_test.go`
- No changes expected (existing tests remain valid)

### Modified: `internal/config/config_test.go` (if exists, else new)
- Test `STALE_THRESHOLD` config file parsing
- Test `CLAUDE_MATRIX_STALE_THRESHOLD` env var override
- Test `DEBUG` config/env parsing

## Implementation Order (Suggested PRs)

The coding expert may choose to implement in a single PR or split into stacked PRs. A suggested order that minimizes merge conflicts:

1. **PR 1 — Marker file module + session creation integration** (Req 1)
   - New `internal/marker/` package
   - New `tmux.GetSessionPane()` method
   - Modify `create.go` and `list.go` for write/cleanup

2. **PR 2 — Hook handler fixes** (Reqs 2, 5, 8, 9)
   - TMUX_PANE fallback via marker
   - Remove idempotency check
   - SessionEnd marker cleanup
   - Error surfacing to stderr
   - Introduce `HandleOptions` struct

3. **PR 3 — Remove process fallback + configurable staleness** (Reqs 3, 4)
   - Delete `analyzeClaudeState` and related functions
   - Simplify `GetDetailedClaudeState`
   - Add `StaleThreshold` to config
   - Update list.go caller

4. **PR 4 — Hook validation + diagnostics + debug logging** (Reqs 5, 6, 7)
   - Fix `isSetupInFile` to check ALL events
   - Add `MissingHookEvents`
   - New `internal/debug/` package
   - Enhance `diagnose` command

This order ensures each PR is independently testable and reviewable.

## Risks and Considerations

1. **Marker file directory collision:** Marker `.env` files share `~/.tmux-claude-matrix/sessions/` with session `.json` files. The different extensions prevent collision, but the `session.Manager.List()` function filters by `.json` extension so it won't be affected.

2. **TMUX_PANE fallback performance:** Scanning all marker files and querying tmux for each is O(N) where N = number of active sessions. With typical usage (<20 sessions), this is negligible. The hook handler is invoked per-event so it must stay fast.

3. **Race condition on marker write:** If `create.go` writes the marker file but the tmux session hasn't fully initialized, the pane query might fail. Mitigation: query the pane after `CreateSession` returns, which blocks until the session exists.

4. **Backward compatibility:** Sessions created before this change won't have marker files. The TMUX_PANE fallback gracefully degrades — if no marker is found and TMUX_PANE is empty, the handler returns an error rather than silently dropping the event. The `diagnose` command helps users identify this.
