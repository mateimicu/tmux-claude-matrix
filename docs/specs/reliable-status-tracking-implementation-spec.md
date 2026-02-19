# Implementation Spec: Reliable Status Tracking

**PRD:** `docs/prd/reliable-status-tracking.md`
**Branch:** `spec/reliable-status-tracking` (stacked on `prd/reliable-status-tracking`)

## Architecture Overview

This feature repairs the hook-based status tracking system. The changes:

1. Remove unreliable process-based fallback detection — state file is the sole source of truth
2. Fix the idempotency short-circuit that suppresses timestamp updates
3. Make the staleness threshold configurable (default 15 min, up from 5)
4. Validate that ALL hook events are registered, not just one
5. Add debug logging and enhance the existing diagnostic command for troubleshooting
6. Surface errors to stderr instead of silently swallowing them
7. Make SessionEnd Teams-safe (write "stopped" state instead of removing the file)

**Deviation from PRD:** The PRD specifies a session marker file as a TMUX_PANE resolution fallback (Req 1). Per user direction, this fallback is **not implemented**. The hook handler requires TMUX_PANE to be set. When it is not, the handler returns a descriptive error (surfaced to stderr and debug log). The `diagnose` command surfaces missing TMUX_PANE as a diagnosable issue.

The changes touch 8 existing files and add 1 new file. No new external dependencies.

## Claude Code Teams Behavior

In Claude Code Teams, the tech lead spawns teammate sub-agents. Each teammate is a separate Claude Code session with its own `session_id` that fires its own hook events (SessionStart, UserPromptSubmit, Stop, SessionEnd, etc.). However, all agents in a Teams session share the **same tmux pane** (same TMUX_PANE value). This means:

- **One state file per tmux session:** The state file is keyed by tmux session name (e.g., `my-repo-1.state`). All teammates write to the same file. The file reflects the **most recent** hook event from any agent in that session.
- **Last-write-wins is correct:** The most recent event from any agent is the best representation of the session's current activity. If the tech lead is running and a teammate goes idle, the tech lead's next `PreToolUse` event overwrites with "running" — which is accurate.
- **SessionEnd must NOT remove the state file:** When a teammate fires SessionEnd (e.g., the teammate is shut down), the current code removes the state file and resets the window name. This is wrong — it nukes the state for the still-running tech lead. The fix: on SessionEnd, **write "stopped" state** (same as any other state transition) instead of removing the file. If other agents are still active, their next event overwrites it immediately.
- **State file cleanup:** State files are cleaned up at session deletion time (existing behavior in `list.go:handleDeleteAction`), not on SessionEnd.

## Component Changes

### 1. Hook Handler: Remove Idempotency, Fix SessionEnd, Surface Errors (PRD Reqs 2, 5, 8, 9)

**Modified file:** `internal/hooks/handler.go`

**Changes to `HandleHookEvent`:**

- **Remove idempotency short-circuit:** Delete the check that skips `WriteState` when state and session ID match. Every hook event must call `WriteState` to refresh the timestamp. This prevents false staleness when the same state is re-entered (e.g., `running -> idle -> running` with the same session ID).

- **TMUX_PANE missing → error instead of silent nil:** When `os.Getenv("TMUX_PANE")` is empty, return a descriptive error instead of `nil`. This surfaces the problem via stderr (cobra's error handling) and debug log.

- **SessionEnd: write "stopped" instead of removing state file (Teams fix):** Replace the current SessionEnd branch (which calls `RemoveState` and renames to plain "claude") with the standard write-state-and-rename-window path. SessionEnd maps to `ClaudeStateStopped`, so the handler writes "stopped" to the state file and sets the window name to "⚫claude". This makes SessionEnd Teams-safe: if other agents are still active, their next event overwrites both the state file and window name.

- **Debug logging integration:** Accept a debug logger and log: event received, TMUX_PANE value, resolved session name, state transition, and any errors. The coding expert decides the exact mechanism for passing the logger (additional parameter, options struct, or package-level logger).

**Modified file:** `cmd/claude-matrix/hook_handler.go`

- Create and pass a debug logger to `HandleHookEvent`.
- Ensure errors are written to stderr. Cobra's `RunE` already does this, but the coding expert should verify and add explicit stderr output if needed.

### 2. Remove Process-Based Fallback (PRD Req 4)

**Modified file:** `internal/tmux/tmux.go`

**Delete these functions entirely:**
- `analyzeClaudeState` — parses pane content for string patterns (inherently unreliable)
- `capturePaneContent` — captures pane output (only used by analyzeClaudeState)
- `getProcessState` — inspects process state via `ps` (only used by GetDetailedClaudeState fallback)
- `getPaneLastActivity` — gets pane timestamp (only used by GetDetailedClaudeState fallback)
- `processIsClaude` — walks process tree looking for "claude" (only used by GetClaudeStatus and getProcessState)
- `GetClaudeStatus` — process-based Claude detection (called from list.go, replaced by state file)

**Simplify `GetDetailedClaudeState`:**

The function should accept a staleness threshold parameter instead of hardcoding `5*time.Minute`. The logic becomes:

1. Read state file → if missing, return `Unknown`
2. If stale (exceeds threshold) → return `Unknown`
3. If valid → return the state from the file

No process inspection. No pane content parsing. `Unknown` for anything the state file can't answer.

**Modified file:** `cmd/claude-matrix/list.go`

- Remove the call to `tmuxMgr.GetClaudeStatus()` — function is deleted
- Derive `ClaudeRunning` from the state value instead
- Pass the configured staleness threshold to `GetDetailedClaudeState`

**Modified file:** `internal/tmux/tmux_test.go`

- Remove `TestAnalyzeClaudeState` — function is deleted
- Keep `TestStripEmojiPrefix`

### 3. Configurable Staleness Threshold (PRD Req 3)

**Modified file:** `pkg/types/types.go`

Add `StaleThreshold time.Duration` to the `Config` struct.

**Modified file:** `internal/config/config.go`

- Default: `15 * time.Minute`
- Config file key: `STALE_THRESHOLD` (value in minutes, e.g., `STALE_THRESHOLD=30`)
- Env var: `CLAUDE_MATRIX_STALE_THRESHOLD` (value in minutes, e.g., `CLAUDE_MATRIX_STALE_THRESHOLD=30`)
- Env var overrides config file, which overrides default (existing precedence pattern)
- Invalid values (non-numeric, zero, negative) fall back to the default

### 4. Validate All Hooks Registered (PRD Req 5/6)

**Modified file:** `internal/hooks/settings.go`

- **Fix `isSetupInFile`:** Currently returns `true` on the first matching event. Change to return `true` only when ALL events in `hookEventDefs` have a matching entry.

- **Add `MissingHookEvents` function:** Returns the list of event names that are not registered in the settings file. Returns `nil` if all are registered. Provide a testable internal variant that accepts an explicit file path.

### 5. Debug Logging (PRD Req 6)

**New file:** `internal/debug/debug.go`

**Existing logging in the codebase:** The only logging pattern is `GitHubSource.logger` in `internal/repos/github.go` — an `io.Writer` used for user-facing progress messages (e.g., "Fetching GitHub repos..."). This is NOT debug logging and serves a different purpose (console output for interactive commands). There is no existing debug log infrastructure, debug mode toggle, or log file mechanism anywhere in the codebase. The `internal/debug/` package is genuinely new.

A thin wrapper around `log.Logger` from stdlib. When enabled, it opens `~/.tmux-claude-matrix/logs/hooks.log` for append and writes timestamped messages. When disabled, all writes are no-ops. A single shared log file (not per-session) is appropriate because the hook handler is a short-lived process invoked per-event — per-session files would add complexity without benefit since events are already tagged with the session name in the log message.

**Enablement:** `CLAUDE_MATRIX_DEBUG=1` env var OR `DEBUG=1` in the config file.

**Modified file:** `pkg/types/types.go` — Add `Debug bool` to `Config`.

**Modified file:** `internal/config/config.go` — Handle `DEBUG` config key and `CLAUDE_MATRIX_DEBUG` env var.

**Integration:** The hook handler (`cmd/claude-matrix/hook_handler.go`) creates the logger and passes it to `HandleHookEvent`. The coding expert decides the passing mechanism.

### 6. Enhance Existing Diagnostic Command (PRD Req 7)

**Modified file:** `cmd/claude-matrix/diagnose.go` (already exists at this path)

The existing `diagnose` command (`claude-matrix diagnose`) currently reports on repository discovery: config values, local repos file status, GitHub API connectivity, and total repository count. This command is enhanced — not replaced — with new hook/status diagnostic sections added **before** the existing repository sections (since hook health is more immediately actionable).

**New diagnostic sections to add:**

1. **Hook registration** — Call `hooks.MissingHookEvents(binaryPath)`. Report each event as registered/missing. Report the binary path from hook commands and whether it resolves to an executable.

2. **State files** — List all `.state` files in the status directory. For each: session name, state value, age.

3. **Active tmux sessions** — List sessions and their window names.

4. **Environment** — Show `TMUX_PANE`, `CLAUDE_MATRIX_DEBUG`, `CLAUDE_MATRIX_STALE_THRESHOLD` values, and the configured stale threshold.

The existing repository diagnostic sections remain unchanged.

## Data Flow Diagrams

### Hook Event Flow

```
Claude Code fires hook event (tech lead OR teammate)
  |
  v
hook_handler.go: HandleHookEvent(stdin, mgr, ...)
  |
  v
Parse JSON event from stdin (includes session_id)
  |
  v
Map event to ClaudeState
  |
  v
TMUX_PANE set?
  |
  YES                              NO
  |                                |
  v                                v
Resolve session name         Return error to stderr
via mgr.GetSessionNameFromPane    + debug log
  |
  v
Write state file (always, including SessionEnd→"stopped")
  |
  v
Rename window "{emoji}claude"
  |
  v
Log to debug log (if enabled)
```

**Teams note:** Multiple agents (tech lead + teammates) share the same TMUX_PANE and write to the same state file. The most recent event wins. A teammate's SessionEnd writes "stopped" but is immediately overwritten if other agents are still active.

### State Reading Flow

```
list.go: GetDetailedClaudeState(session, staleThreshold)
  |
  v
Read state file
  |
  +--> File missing? --> return Unknown, zero time
  +--> File stale?   --> return Unknown, file's UpdatedAt
  +--> Valid?        --> return state, file's UpdatedAt
```

### Session Deletion Flow

```
list.go: handleDeleteAction()
  |
  v
Kill tmux session             [existing]
  |
  v
sessionMgr.Delete(name)      [existing]
  |
  v
status.RemoveState(dir, name) [existing — this is where state file gets removed]
```

## File Change Summary

| File | Change | PRD Reqs |
|------|--------|----------|
| `internal/debug/debug.go` | **NEW** | 6 |
| `internal/hooks/handler.go` | MODIFY | 2, 5, 6, 8, 9 |
| `internal/hooks/settings.go` | MODIFY | 5 |
| `internal/tmux/tmux.go` | MODIFY (major — delete 6 functions) | 3, 4 |
| `internal/config/config.go` | MODIFY | 3, 6 |
| `pkg/types/types.go` | MODIFY | 3, 6 |
| `cmd/claude-matrix/hook_handler.go` | MODIFY | 6, 9 |
| `cmd/claude-matrix/list.go` | MODIFY | 3, 4 |
| `cmd/claude-matrix/diagnose.go` | MODIFY (enhance existing) | 7 |

## Test Plan

### Coverage targets

All new and modified code should maintain the existing test patterns in the codebase. Every behavioral change listed below needs at least one test. The coding expert should aim for branch coverage of the modified functions.

### `internal/debug/debug_test.go` (new)

| Scenario | Given | When | Then |
|----------|-------|------|------|
| Enabled logger writes | `CLAUDE_MATRIX_DEBUG=1` | `Log("msg")` called | Message appears in log file with timestamp |
| Disabled logger is no-op | Debug disabled | `Log("msg")` called | No file created, no error |
| Nil logger safety | Logger is nil | Caller attempts to log | No panic (coding expert decides: nil check or guaranteed non-nil) |

### `internal/hooks/handler_test.go` (modified)

| Scenario | Given | When | Then |
|----------|-------|------|------|
| Idempotency removed | State file has `running` + same session ID | `UserPromptSubmit` event fires | State file is rewritten with fresh timestamp |
| TMUX_PANE missing | `TMUX_PANE` env var is empty | Any hook event fires | Returns non-nil error containing "TMUX_PANE" |
| SessionEnd writes stopped | State file exists, TMUX_PANE is set | `SessionEnd` event fires | State file contains "stopped" (NOT removed), window renamed to "⚫claude" |
| SessionEnd with missing state file | No state file exists | `SessionEnd` event fires | State file created with "stopped", no error |
| Teams: teammate SessionEnd overwritten | Teammate fires SessionEnd (writes "stopped") | Tech lead fires PreToolUse | State file updated to "running", window to "🟢claude" |
| Invalid JSON input | Stdin contains malformed JSON | `HandleHookEvent` called | Returns parse error |
| Unknown event type | Event has unrecognized `hook_event_name` | `HandleHookEvent` called | Maps to `Unknown` state, still writes state file |

### `internal/hooks/settings_test.go` (modified)

| Scenario | Given | When | Then |
|----------|-------|------|------|
| All events registered | Settings file has all 6 hook events | `isSetupInFile` called | Returns `true` |
| Partial registration | Settings file has only 3 of 6 events | `isSetupInFile` called | Returns `false` |
| No events registered | Empty settings file | `isSetupInFile` called | Returns `false` |
| Missing events list | Settings file has 4 of 6 events | `MissingHookEvents` called | Returns the 2 missing event names |
| All present | Settings file has all events | `MissingHookEvents` called | Returns `nil` |

### `internal/tmux/tmux_test.go` (modified)

| Scenario | Given | When | Then |
|----------|-------|------|------|
| State file present and fresh | Valid state file, age < threshold | `GetDetailedClaudeState` called | Returns state from file |
| State file stale | Valid state file, age > threshold | `GetDetailedClaudeState` called | Returns `Unknown` |
| State file missing | No state file for session | `GetDetailedClaudeState` called | Returns `Unknown`, zero time |
| Staleness boundary | State file age == threshold exactly | `GetDetailedClaudeState` called | Returns the state (not stale; stale is strictly >) |
| `stripEmojiPrefix` | (existing tests) | — | — (keep as-is) |
| Remove `TestAnalyzeClaudeState` | — | — | Delete (function removed) |

### `internal/config/config_test.go` (new or modified)

| Scenario | Given | When | Then |
|----------|-------|------|------|
| Default stale threshold | No config, no env var | `Load()` | `StaleThreshold` is 15 min |
| Config file override | `STALE_THRESHOLD=30` in config | `Load()` | `StaleThreshold` is 30 min |
| Env var override | `CLAUDE_MATRIX_STALE_THRESHOLD=45` | `Load()` | `StaleThreshold` is 45 min |
| Env var beats config | Config has 30, env var has 45 | `Load()` | `StaleThreshold` is 45 min |
| Invalid threshold value | `STALE_THRESHOLD=abc` in config | `Load()` | Falls back to default (15 min) |
| Zero threshold | `STALE_THRESHOLD=0` in config | `Load()` | Falls back to default (15 min) |
| Debug enabled via env | `CLAUDE_MATRIX_DEBUG=1` | `Load()` | `Debug` is true |
| Debug enabled via config | `DEBUG=1` in config | `Load()` | `Debug` is true |

### Backward compatibility

| Scenario | Given | When | Then |
|----------|-------|------|------|
| Pre-existing sessions | Sessions created before this change (no state files) | `list` command runs | Shows `Unknown` state (not `Stopped`) |
| Partial hook registration | User has old settings.json with fewer events | `IsSetup` called | Returns `false`; `diagnose` lists missing events |

## Implementation Order (Suggested PRs)

The coding expert may implement in a single PR or split. A suggested grouping:

1. **PR 1 — Core fixes** (Reqs 2, 3, 4, 5, 8, 9)
   - Remove process fallback, simplify `GetDetailedClaudeState`
   - Remove idempotency short-circuit in handler
   - Change SessionEnd to write "stopped" instead of removing state file (Teams fix)
   - Add configurable staleness threshold
   - Fix `isSetupInFile` to check all events, add `MissingHookEvents`
   - Surface errors to stderr
   - Update `list.go` caller

2. **PR 2 — Diagnostics and debug logging** (Reqs 6, 7)
   - New `internal/debug/` package (wraps stdlib `log.Logger`)
   - Integrate debug logging into hook handler
   - Enhance existing `diagnose` command with hook/status sections

## Risks and Considerations

1. **Sessions without TMUX_PANE:** Claude Code Teams sub-agents may run in child processes without TMUX_PANE. Without the fallback, these will produce errors in stderr and show `Unknown` state. The `diagnose` command surfaces this. If this proves to be a common problem in practice, a follow-up change can add a resolution mechanism.

2. **Teams SessionEnd race window:** When a teammate fires SessionEnd, the state file briefly shows "stopped" until the next event from a still-running agent overwrites it. This window is typically sub-second (next PreToolUse or UserPromptSubmit fires quickly). The `list` command may rarely catch this intermediate state — acceptable since it's transient and self-correcting.

3. **Backward compatibility:** Sessions created before this change won't have state files written by hooks if hooks weren't registered. They will show `Unknown` instead of the previous (unreliable) `Stopped`. This is an improvement — `Unknown` is honest, `Stopped` was often wrong.

4. **Config validation:** Invalid staleness threshold values (zero, negative, non-numeric) must fall back to the default rather than causing errors. The coding expert should handle this in the config parsing.
