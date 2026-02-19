# Implementation Spec: Reliable Status Tracking

**PRD:** `docs/prd/reliable-status-tracking.md`
**Branch:** `spec/reliable-status-tracking` (stacked on `prd/reliable-status-tracking`)

## Architecture Overview

This feature repairs the hook-based status tracking system. The changes:

1. Multi-agent state model — each teammate tracked individually, aggregate state computed
2. Remove unreliable process-based fallback detection — state file is the sole source of truth
3. Fix the idempotency short-circuit that suppresses timestamp updates
4. Make the staleness threshold configurable (default 15 min, up from 5)
5. Validate that ALL hook events are registered, not just one
6. Add debug logging and enhance the existing diagnostic command for troubleshooting
7. Surface errors to stderr instead of silently swallowing them

**Deviation from PRD:** The PRD specifies a session marker file as a TMUX_PANE resolution fallback (Req 1). Per user direction, this fallback is **not implemented**. The hook handler requires TMUX_PANE to be set. When it is not, the handler returns a descriptive error (surfaced to stderr and debug log). The `diagnose` command surfaces missing TMUX_PANE as a diagnosable issue.

The changes touch 8 existing files and add 1 new file. No new external dependencies.

## Multi-Agent State Model

### Problem

In Claude Code Teams, the tech lead spawns teammate sub-agents. Each teammate is a separate Claude Code session with its own `session_id` that fires its own hook events. All agents in a Teams session share the **same tmux pane** (same TMUX_PANE value) and therefore write to the same state file (keyed by tmux session name).

The old model uses a single state value — last-write-wins. This has two problems:
- A teammate's SessionEnd removes the state file, nuking state for still-running agents
- A teammate going idle overwrites the tech lead's "running" state, showing incorrect status

### Solution: Per-Agent Entries with Aggregate State

The state file tracks each agent independently, keyed by its Claude Code `session_id`. The displayed state is computed by aggregating all non-stale agent entries.

**New state file format:**

```json
{
  "agents": {
    "sess-abc-123": {
      "state": "running",
      "updated_at": "2026-02-19T10:30:00Z"
    },
    "sess-def-456": {
      "state": "idle",
      "updated_at": "2026-02-19T10:29:55Z"
    }
  }
}
```

**Aggregation priority** (highest wins):
1. `running` — any agent actively processing
2. `waiting_for_input` — any agent needs user attention
3. `idle` — all agents idle (none running or waiting)
4. `error` — only errors remain
5. `stopped` — all agents stopped or map is empty
6. `unknown` — file missing or unreadable

If ANY agent is `running`, the aggregate is `running` regardless of other agents' states. If ANY agent is `waiting_for_input` (and none running), the aggregate is `waiting_for_input`. And so on down the priority list.

**Per-agent staleness:** Each agent entry has its own `updated_at`. Entries that exceed the staleness threshold are excluded from aggregation. This handles crashed agents that never fired SessionEnd — their entries go stale and stop affecting the aggregate.

**SessionEnd per-agent:** When a teammate fires SessionEnd, only THAT agent's entry is removed from the map. Other agents' entries are preserved. When the map becomes empty after the last agent's SessionEnd, the file can be removed.

### Backward Compatibility

The reader must handle both the old format and the new format:
- **Old format** (has `"state"` key at top level): Treat as single-agent. Use the old `session_id` field as the agent key in the converted map (or a sentinel like `"legacy"` if `session_id` is empty). The first write from the new code overwrites with the multi-agent format.
- **New format** (has `"agents"` key): Use multi-agent logic.

### Concurrency

Multiple hook handlers (from different agents) may run concurrently, each performing a read-modify-write cycle on the same state file. If two agents read simultaneously, modify their own entry, and write back, one agent's update can be lost (classic read-modify-write race).

The coding expert should use **file locking** (`syscall.Flock` or `golang.org/x/sys/unix` flock) around the read-modify-write cycle. The lock should be advisory, on a separate `.lock` file next to the state file. If file locking proves problematic on the target platform, the fallback is to accept the rare race — the lost update is transient and self-corrects on the next event from that agent.

## Component Changes

### 1. State File: Multi-Agent Format (new model)

**Modified file:** `internal/status/status.go`

The state file format and API change substantially. The coding expert implements the details; the key contracts are:

**New data structures:**
- `AgentState` — per-agent entry with `State` and `UpdatedAt` fields
- `StateFile` — contains a map of agent ID → `AgentState`

**New/changed functions:**

- **`UpdateAgentState(statusDir, sessionName, agentID string, state ClaudeState) error`** — Read-modify-write: reads existing state file (or creates empty), updates the entry for `agentID`, writes back atomically. This replaces `WriteState`.

- **`RemoveAgentState(statusDir, sessionName, agentID string) error`** — Read-modify-write: reads state file, removes the entry for `agentID`, writes back. If the map is empty after removal, removes the file entirely.

- **`ReadStateFile(statusDir, sessionName string) (*StateFile, error)`** — Reads and parses the state file. Handles both old format (converts to single-agent map) and new format.

- **`ComputeState(sf *StateFile, staleThreshold time.Duration) (ClaudeState, time.Time)`** — Iterates agent entries, excludes stale ones, returns the highest-priority state and the most recent `updated_at` among non-stale entries. Returns `unknown` if all entries are stale (agents exist but none reporting). Returns `stopped` if the agents map is empty (all agents have ended).

- **Keep unchanged:** `RemoveState`, `DefaultStatusDir`, `EmojiForState`, `stateFilePath`.

- **Remove:** `IsStale` (replaced by per-agent staleness in `ComputeState`). Or keep for internal use if the coding expert finds it useful.

### 2. Hook Handler: Per-Agent Writes, Surface Errors (PRD Reqs 2, 5, 8, 9)

**Modified file:** `internal/hooks/handler.go`

**Changes to `HandleHookEvent`:**

- **Per-agent state writes:** Instead of overwriting the entire state file, call `UpdateAgentState` with the event's `session_id` as the agent ID. This ensures each teammate's state is tracked independently.

- **Remove idempotency short-circuit:** Delete the check that skips writes when state and session ID match. Every hook event must update the agent's timestamp.

- **SessionEnd per-agent:** On SessionEnd, call `RemoveAgentState` with the event's `session_id`. This removes only that agent's entry, preserving other agents' states.

- **Compute aggregate for window name:** After updating the agent state, call `ComputeState` to get the aggregate state, then set the window emoji accordingly.

- **TMUX_PANE missing → error instead of silent nil:** When TMUX_PANE is empty, return a descriptive error instead of `nil`.

- **Debug logging integration:** Log event received, agent session_id, TMUX_PANE value, resolved session name, per-agent state update, computed aggregate state, and any errors. The coding expert decides the passing mechanism.

**Modified file:** `cmd/claude-matrix/hook_handler.go`

- Create and pass a debug logger to `HandleHookEvent`.
- Ensure errors are written to stderr.

### 3. Remove Process-Based Fallback (PRD Req 4)

**Modified file:** `internal/tmux/tmux.go`

**Delete these functions entirely:**
- `analyzeClaudeState` — parses pane content for string patterns
- `capturePaneContent` — captures pane output
- `getProcessState` — inspects process state via `ps`
- `getPaneLastActivity` — gets pane timestamp
- `processIsClaude` — walks process tree
- `GetClaudeStatus` — process-based Claude detection

**Simplify `GetDetailedClaudeState`:**

The function should accept a staleness threshold parameter. The logic becomes:

1. Read state file via `ReadStateFile`
2. If error (file missing) → return `Unknown`, zero time
3. Call `ComputeState(sf, staleThreshold)` → return aggregate state and timestamp

**Modified file:** `cmd/claude-matrix/list.go`

- Remove the call to `tmuxMgr.GetClaudeStatus()` — function is deleted
- Derive `ClaudeRunning` from the state value instead
- Pass the configured staleness threshold to `GetDetailedClaudeState`

**Modified file:** `internal/tmux/tmux_test.go`

- Remove `TestAnalyzeClaudeState` — function is deleted
- Keep `TestStripEmojiPrefix`

### 4. Configurable Staleness Threshold (PRD Req 3)

**Modified file:** `pkg/types/types.go`

Add `StaleThreshold time.Duration` to the `Config` struct.

**Modified file:** `internal/config/config.go`

- Default: `15 * time.Minute`
- Config file key: `STALE_THRESHOLD` (value in minutes, e.g., `STALE_THRESHOLD=30`)
- Env var: `CLAUDE_MATRIX_STALE_THRESHOLD` (value in minutes, e.g., `CLAUDE_MATRIX_STALE_THRESHOLD=30`)
- Env var overrides config file, which overrides default (existing precedence pattern)
- Invalid values (non-numeric, zero, negative) fall back to the default

### 5. Validate All Hooks Registered (PRD Req 5/6)

**Modified file:** `internal/hooks/settings.go`

- **Fix `isSetupInFile`:** Currently returns `true` on the first matching event. Change to return `true` only when ALL events in `hookEventDefs` have a matching entry.

- **Add `MissingHookEvents` function:** Returns the list of event names that are not registered in the settings file. Returns `nil` if all are registered. Provide a testable internal variant that accepts an explicit file path.

### 6. Debug Logging (PRD Req 6)

**New file:** `internal/debug/debug.go`

**Existing logging in the codebase:** The only logging pattern is `GitHubSource.logger` in `internal/repos/github.go` — an `io.Writer` used for user-facing progress messages. This is NOT debug logging. There is no existing debug log infrastructure anywhere in the codebase. The `internal/debug/` package is genuinely new.

A thin wrapper around `log.Logger` from stdlib. When enabled, it opens `~/.tmux-claude-matrix/logs/hooks.log` for append and writes timestamped messages. When disabled, all writes are no-ops. A single shared log file (not per-session) — events are tagged with the session name and agent ID in each log message.

**Enablement:** `CLAUDE_MATRIX_DEBUG=1` env var OR `DEBUG=1` in the config file.

**Modified file:** `pkg/types/types.go` — Add `Debug bool` to `Config`.

**Modified file:** `internal/config/config.go` — Handle `DEBUG` config key and `CLAUDE_MATRIX_DEBUG` env var.

### 7. Enhance Existing Diagnostic Command (PRD Req 7)

**Modified file:** `cmd/claude-matrix/diagnose.go` (already exists at this path)

The existing `diagnose` command (`claude-matrix diagnose`) currently reports on repository discovery. This command is enhanced — not replaced — with new hook/status diagnostic sections added **before** the existing repository sections.

**New diagnostic sections to add:**

1. **Hook registration** — Call `hooks.MissingHookEvents(binaryPath)`. Report each event as registered/missing. Report the binary path and whether it resolves to an executable.

2. **State files** — List all `.state` files in the status directory. For each: session name, per-agent entries (agent ID, state, age), and computed aggregate state.

3. **Active tmux sessions** — List sessions and their window names.

4. **Environment** — Show `TMUX_PANE`, `CLAUDE_MATRIX_DEBUG`, `CLAUDE_MATRIX_STALE_THRESHOLD` values, and the configured stale threshold.

## Data Flow Diagrams

### Hook Event Flow (per-agent)

```
Claude Code fires hook event (tech lead OR teammate)
  |
  v
Parse JSON event from stdin
  → extract: session_id (agent ID), hook_event_name
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
Resolve tmux session name    Return error to stderr
  |                               + debug log
  v
SessionEnd?
  |
  YES                              NO
  |                                |
  v                                v
RemoveAgentState(             UpdateAgentState(
  dir, session,                 dir, session,
  event.session_id)             event.session_id,
  |                             state)
  |                                |
  v                                v
File deleted                  ReadStateFile →
(last agent)?                   ComputeState(staleThreshold)
  |                                |
  YES         NO                   v
  |           |              Rename window
  v           v              "{emoji}claude"
Rename     ReadStateFile →      (based on aggregate)
"⚫claude"  ComputeState →
(stopped)   Rename window
             "{emoji}claude"
  |           |                    |
  +-----------+--------------------+
                   |
                   v
       Log to debug log (if enabled)
```

### State Aggregation Example (Teams)

```
State file agents map:
  "sess-lead-001":  running       (updated 2s ago)
  "sess-team-002":  idle          (updated 5s ago)
  "sess-team-003":  stopped       (updated 1s ago — just fired SessionEnd,
                                    entry about to be removed)

After RemoveAgentState for sess-team-003:
  "sess-lead-001":  running       (updated 2s ago)
  "sess-team-002":  idle          (updated 5s ago)

ComputeState → "running" (highest priority among non-stale entries)
Window → "🟢claude"
```

### State Reading Flow

```
list.go: GetDetailedClaudeState(session, staleThreshold)
  |
  v
ReadStateFile(statusDir, session)
  |
  +--> File missing?  --> return Unknown, zero time
  +--> Old format?    --> convert to single-agent map, then compute
  +--> New format?    --> ComputeState(sf, staleThreshold)
                           |
                           +--> All entries stale?  --> return Unknown
                           +--> Map empty?          --> return Stopped
                           +--> Has non-stale?      --> return aggregate state
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
status.RemoveState(dir, name) [existing — removes the whole state file]
```

## File Change Summary

| File | Change | PRD Reqs |
|------|--------|----------|
| `internal/debug/debug.go` | **NEW** | 6 |
| `internal/status/status.go` | MODIFY (major — new multi-agent format) | 2, 8 |
| `internal/hooks/handler.go` | MODIFY (per-agent writes, remove idempotency) | 2, 5, 6, 8, 9 |
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

### `internal/status/status_test.go` (major changes)

| Scenario | Given | When | Then |
|----------|-------|------|------|
| Update single agent | Empty state file | `UpdateAgentState(dir, sess, "agent-1", running)` | File contains agents map with one entry |
| Update multiple agents | State file has agent-1 as running | `UpdateAgentState(dir, sess, "agent-2", idle)` | File has both agents, each with correct state |
| Update existing agent | Agent-1 is idle | `UpdateAgentState(dir, sess, "agent-1", running)` | Agent-1 updated to running with new timestamp |
| Remove agent | State file has agent-1 and agent-2 | `RemoveAgentState(dir, sess, "agent-1")` | Only agent-2 remains in file |
| Remove last agent | State file has only agent-1 | `RemoveAgentState(dir, sess, "agent-1")` | State file is deleted |
| Remove nonexistent agent | State file has agent-1 | `RemoveAgentState(dir, sess, "agent-99")` | No error, agent-1 unchanged |
| Compute: running wins | agent-1=running, agent-2=idle | `ComputeState(sf, 15m)` | Returns `running` |
| Compute: waiting wins over idle | agent-1=waiting, agent-2=idle | `ComputeState(sf, 15m)` | Returns `waiting_for_input` |
| Compute: idle when all idle | agent-1=idle, agent-2=idle | `ComputeState(sf, 15m)` | Returns `idle` |
| Compute: stale entries excluded | agent-1=running (stale), agent-2=idle (fresh) | `ComputeState(sf, 15m)` | Returns `idle` (running agent ignored) |
| Compute: all stale | all entries exceed threshold | `ComputeState(sf, 15m)` | Returns `unknown` |
| Compute: empty map | no agents in file | `ComputeState(sf, 15m)` | Returns `stopped` |
| Backward compat: old format | File has `{"state":"running","updated_at":"...","session_id":"x"}` | `ReadStateFile` | Returns StateFile with one agent entry |
| Timestamp: most recent | agent-1 updated 2s ago, agent-2 updated 10s ago | `ComputeState` | Returns agent-1's timestamp |

### `internal/debug/debug_test.go` (new)

| Scenario | Given | When | Then |
|----------|-------|------|------|
| Enabled logger writes | `CLAUDE_MATRIX_DEBUG=1` | `Log("msg")` called | Message appears in log file with timestamp |
| Disabled logger is no-op | Debug disabled | `Log("msg")` called | No file created, no error |
| Nil logger safety | Logger is nil | Caller attempts to log | No panic |

### `internal/hooks/handler_test.go` (modified)

| Scenario | Given | When | Then |
|----------|-------|------|------|
| Per-agent write | Empty state file | Agent "sess-1" fires UserPromptSubmit | State file has "sess-1" entry with "running" |
| Multiple agents | "sess-1" is running | Agent "sess-2" fires SessionStart (idle) | State file has both agents; aggregate is "running" |
| Idempotency removed | "sess-1" already running | "sess-1" fires PreToolUse (still running) | Timestamp updated (not skipped) |
| TMUX_PANE missing | TMUX_PANE env var is empty | Any hook event fires | Returns non-nil error containing "TMUX_PANE" |
| SessionEnd per-agent | "sess-1"=running, "sess-2"=idle | "sess-2" fires SessionEnd | "sess-2" removed, "sess-1" remains; aggregate is "running", window "🟢claude" |
| SessionEnd last agent | Only "sess-1" exists | "sess-1" fires SessionEnd | Agent removed, file removed, window "⚫claude" |
| Invalid JSON input | Stdin contains malformed JSON | `HandleHookEvent` called | Returns parse error |
| Unknown event type | Event has unrecognized name | `HandleHookEvent` called | Maps to `Unknown` state, still writes agent entry |

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
| Multi-agent state fresh | State file with 2 agents, both fresh | `GetDetailedClaudeState` called | Returns aggregate state |
| All agents stale | State file with agents, all stale | `GetDetailedClaudeState` called | Returns `Unknown` |
| State file missing | No state file for session | `GetDetailedClaudeState` called | Returns `Unknown`, zero time |
| Old format file | State file in old single-state format | `GetDetailedClaudeState` called | Returns state (backward compat) |
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
| Old-format state file | State file in `{"state":"running","updated_at":"..."}` format | `ReadStateFile` + `ComputeState` | Returns correct state (treated as single agent) |
| First new write over old file | Old-format state file exists | `UpdateAgentState` called | File overwritten with new multi-agent format |
| Pre-existing sessions (no file) | No state files at all | `list` command runs | Shows `Unknown` state |
| Partial hook registration | Old settings.json with fewer events | `IsSetup` called | Returns `false`; `diagnose` lists missing events |

## Implementation Order (Suggested PRs)

The coding expert may implement in a single PR or split. A suggested grouping:

1. **PR 1 — Multi-agent state model + core fixes** (Reqs 2, 3, 4, 5, 8, 9)
   - New state file format with per-agent entries in `status.go`
   - Per-agent writes and SessionEnd removal in `handler.go`
   - Aggregate state computation with priority rules
   - Remove process fallback, simplify `GetDetailedClaudeState`
   - Configurable staleness threshold
   - Fix `isSetupInFile` to check all events, add `MissingHookEvents`
   - Surface errors to stderr
   - Update `list.go` caller
   - Backward compatibility with old state file format

2. **PR 2 — Diagnostics and debug logging** (Reqs 6, 7)
   - New `internal/debug/` package (wraps stdlib `log.Logger`)
   - Integrate debug logging into hook handler
   - Enhance existing `diagnose` command with hook/status sections (show per-agent state)

## Risks and Considerations

1. **Concurrency on state file:** Multiple hook handlers from different agents may perform concurrent read-modify-write cycles. File locking (`flock`) is recommended. If locking is omitted, a lost update is transient — the affected agent's next event corrects it within milliseconds.

2. **Sessions without TMUX_PANE:** Teams sub-agents may lack TMUX_PANE. Without a fallback, these produce errors in stderr and their events are dropped. The `diagnose` command surfaces this. The aggregate state still reflects agents that DO have TMUX_PANE.

3. **Backward compatibility:** Old state files (single-state format) are read correctly and converted on first new write. No migration step required.

4. **State file growth:** In a Teams session with many short-lived teammates, entries accumulate until SessionEnd removes them. Stale entries are excluded from aggregation but remain in the file until the next write prunes them or the session is deleted. The coding expert may choose to prune stale entries during `UpdateAgentState` writes.

5. **Config validation:** Invalid staleness threshold values (zero, negative, non-numeric) must fall back to the default rather than causing errors.
