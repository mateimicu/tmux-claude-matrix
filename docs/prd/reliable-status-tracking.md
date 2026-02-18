# PRD: Reliable Status Tracking

## Goal

Make Claude agent status tracking in tmux-claude-matrix reliable for both single-agent and Claude Code Teams (multi-agent) sessions. Currently, status frequently shows as "dark/stopped" (black circle) when agents are actually active. With 10+ concurrent sessions, accurate status is critical for workflow management.

The fix focuses on making the hook-based system reliable and adding diagnostics to troubleshoot failures. The polling/process-based fallback detection will be removed.

## Problem Analysis

Code review identified these root causes in the current hook system:

1. **Silent `TMUX_PANE` failures in both single-agent and Teams mode** (`internal/hooks/handler.go:61-64`): When `TMUX_PANE` is empty, the handler returns `nil` silently — the event is dropped with no error and no log. This affects Teams mode especially, where sub-agents run in child processes that may not inherit `TMUX_PANE`, but can also occur in single-agent sessions if the environment variable is unset for any reason.

2. **Idempotency check suppresses valid transitions** (`internal/hooks/handler.go:80-82`): If state and session ID match the current file, the update is skipped. This means a `running -> idle -> running` sequence with the same session ID won't update the timestamp on the second `running`, potentially causing false staleness.

3. **5-minute staleness threshold** (`internal/tmux/tmux.go:214`): Long-running Claude operations (large code generation, multi-file refactors) may not emit hook events for >5 minutes, incorrectly marking the state as stale and falling through to the unreliable process-based fallback.

4. **Fragile fallback detection** (`internal/tmux/tmux.go:353-413`): `analyzeClaudeState` parses pane content looking for string patterns ("Error:", "Continue?", "Done"). This is inherently unreliable — Claude's output format changes and these patterns appear in normal code output.

5. **No error logging**: The hook handler has no way to report failures. Binary path errors, stdin parse failures, tmux command failures — all silently swallowed.

6. **Incomplete hook registration check** (`internal/hooks/settings.go:123-130`): `isSetupInFile` returns `true` on the first matching event, but doesn't verify ALL required events are registered. A partial registration looks "setup" but misses events.

## Requirements

1. **Add a session marker file for `TMUX_PANE` resolution**: At session creation time, write a marker file at `~/.tmux-claude-matrix/sessions/{sessionName}.env` containing the `TMUX_PANE` value and session name. When the hook handler runs and `TMUX_PANE` is not set in the environment, it reads this marker file to resolve the correct session. This is the sole fallback mechanism for `TMUX_PANE` resolution — no process tree traversal. The marker file must be cleaned up when the session is destroyed.

2. **Remove the idempotency short-circuit**: Always update the state file timestamp on every hook event, even if the state value hasn't changed. This ensures the staleness check reflects actual hook activity, not just state transitions.

3. **Increase staleness threshold to 15 minutes, configurable via `CLAUDE_MATRIX_STALE_THRESHOLD`**: Increase the default staleness threshold from 5 minutes to 15 minutes. Allow override via the `CLAUDE_MATRIX_STALE_THRESHOLD` environment variable (value in minutes, e.g., `CLAUDE_MATRIX_STALE_THRESHOLD=30`). The config file (`~/.config/tmux-claude-matrix/config`) should also support a `STALE_THRESHOLD` field with the same semantics.

4. **Remove the process-based fallback detection**: Remove `analyzeClaudeState`, `capturePaneContent`, `getProcessState`, and related process-inspection code from `internal/tmux/tmux.go`. When the state file is missing or stale, return `unknown` rather than attempting unreliable process detection. **Graceful degradation note:** Sessions without working hooks will display the `unknown` state (white question mark). The `claude-matrix diagnose` command (Req 7) is the recovery path — users run it to identify and fix hook registration or environment issues.

5. **Validate all hooks are registered**: Change `isSetupInFile` to verify that ALL required hook events are registered, not just one. Provide a clear error or warning when hooks are partially registered.

6. **Add debug logging**: Implement a structured logging system for the hook handler that writes to a log file at `~/.tmux-claude-matrix/logs/hooks.log`. Log every hook event received, every state transition, every `TMUX_PANE` resolution attempt (including marker file fallback), and every error. Enable via `CLAUDE_MATRIX_DEBUG=1` environment variable or config setting.

7. **Add a diagnostic command**: Add `claude-matrix diagnose` that reports:
   - Whether hooks are registered in `~/.claude/settings.json` (all events, not just any)
   - The binary path in the hook commands and whether it resolves to an executable
   - Current state files and their ages
   - Active tmux sessions and their window names
   - Session marker files and whether they reference valid tmux panes
   - The `TMUX_PANE` value in the current environment

8. **Handle `SessionEnd` cleanup**: When a `SessionEnd` event fires, remove the state file and reset the tmux window name to plain "claude". This must work in Teams mode even if sub-agents have already exited — the handler uses the marker file for session resolution when `TMUX_PANE` is unavailable.

9. **Log hook handler errors to stderr**: In addition to the debug log file, the hook handler should write errors to stderr so they appear in Claude Code's hook error output (if any), rather than silently returning nil.

## Acceptance Criteria

### Core status tracking
- [ ] Status correctly reflects `running`, `idle`, `waiting_for_input`, and `stopped` states in single-agent sessions
- [ ] Status correctly reflects states in Claude Code Teams sessions (multi-agent)
- [ ] When `TMUX_PANE` is not set (Teams sub-agent context), the hook handler resolves the correct session via the marker file and updates status

### Session marker file
- [ ] A marker file is created at `~/.tmux-claude-matrix/sessions/{sessionName}.env` when a session is created, containing the `TMUX_PANE` value and session name
- [ ] The marker file correctly maps the session name to its tmux pane
- [ ] The marker file is removed when the session is destroyed (via `claude-matrix` delete or `KillSession`)

### Idempotency and staleness
- [ ] Idempotency check no longer suppresses timestamp updates — staleness is based on last hook event, not last state change
- [ ] Default staleness threshold is 15 minutes
- [ ] Staleness threshold is configurable via `CLAUDE_MATRIX_STALE_THRESHOLD` env var (value in minutes) and `STALE_THRESHOLD` config file field

### Fallback removal and graceful degradation
- [ ] Process-based fallback detection (`analyzeClaudeState`, `capturePaneContent`, `getProcessState`, and related code) is removed from `internal/tmux/tmux.go`
- [ ] Sessions without working hooks display `unknown` state (white question mark) instead of incorrect `stopped`

### Hook registration validation
- [ ] `isSetupInFile` validates ALL required hook events are registered, not just the first match
- [ ] A clear warning is shown when hooks are partially registered

### SessionEnd cleanup
- [ ] When `SessionEnd` fires, the state file is removed
- [ ] When `SessionEnd` fires, the tmux window name is reset to plain "claude"
- [ ] SessionEnd cleanup works in Teams mode when `TMUX_PANE` is unavailable (falls back to marker file)

### Diagnostics
- [ ] `claude-matrix diagnose` command exists and reports hook registration status, binary path validity, state file ages, marker file validity, and environment
- [ ] Debug logging can be enabled via `CLAUDE_MATRIX_DEBUG=1` and writes to `~/.tmux-claude-matrix/logs/hooks.log`
- [ ] Hook handler errors are written to stderr (not silently swallowed)

### Testing and regressions
- [ ] All existing tests pass; new tests cover marker file creation/cleanup, `TMUX_PANE` resolution fallback, staleness threshold configuration, and the diagnostic command
- [ ] No regressions in session creation, listing, or deletion workflows

## Out of Scope

- Polling/process-based status detection as a fallback mechanism (explicitly removed)
- UI/UX changes to the FZF selection interface
- Changes to the tmux status bar format or emoji set
- Support for non-tmux terminal multiplexers
- New real-time status push mechanisms (e.g., tmux control mode, websockets, inotify/fswatch) — the existing hook-based system is being repaired, not replaced with a different push architecture
- Changes to session creation or destruction workflows beyond adding/removing the marker file
- Performance optimization of hook handler execution
