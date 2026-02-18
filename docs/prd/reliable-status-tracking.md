# PRD: Reliable Status Tracking

## Goal

Make Claude agent status tracking in tmux-claude-matrix reliable for both single-agent and Claude Code Teams (multi-agent) sessions. Currently, status frequently shows as "dark/stopped" (black circle) when agents are actually active. With 10+ concurrent sessions, accurate status is critical for workflow management.

The fix focuses on making the hook-based system reliable and adding diagnostics to troubleshoot failures. The polling/process-based fallback detection will be removed.

## Problem Analysis

Code review identified these root causes in the current hook system:

1. **Silent `TMUX_PANE` failures** (`internal/hooks/handler.go:61-64`): When `TMUX_PANE` is empty, the handler returns `nil` silently — the event is dropped with no error and no log. In Teams mode, sub-agents may run in contexts where `TMUX_PANE` isn't inherited.

2. **Idempotency check suppresses valid transitions** (`internal/hooks/handler.go:80-82`): If state and session ID match the current file, the update is skipped. This means a `running -> idle -> running` sequence with the same session ID won't update the timestamp on the second `running`, potentially causing false staleness.

3. **5-minute staleness threshold** (`internal/tmux/tmux.go:214`): Long-running Claude operations (large code generation, multi-file refactors) may not emit hook events for >5 minutes, incorrectly marking the state as stale and falling through to the unreliable process-based fallback.

4. **Fragile fallback detection** (`internal/tmux/tmux.go:353-413`): `analyzeClaudeState` parses pane content looking for string patterns ("Error:", "Continue?", "Done"). This is inherently unreliable — Claude's output format changes and these patterns appear in normal code output.

5. **No error logging**: The hook handler has no way to report failures. Binary path errors, stdin parse failures, tmux command failures — all silently swallowed.

6. **Incomplete hook registration check** (`internal/hooks/settings.go:123-130`): `isSetupInFile` returns `true` on the first matching event, but doesn't verify ALL required events are registered. A partial registration looks "setup" but misses events.

7. **Teams sub-agent environment**: When Claude Code spawns sub-agents (Teams mode), hook events fire from child processes that may not inherit the `TMUX_PANE` environment variable, causing all their state updates to be silently dropped.

## Requirements

1. **Fix `TMUX_PANE` resolution for Teams mode**: When `TMUX_PANE` is not set, the hook handler must attempt to resolve the session name through alternative means (e.g., traversing the process tree to find the parent tmux pane, or reading a session marker file written at session creation time).

2. **Remove the idempotency short-circuit**: Always update the state file timestamp on every hook event, even if the state value hasn't changed. This ensures the staleness check reflects actual hook activity, not just state transitions.

3. **Make staleness threshold configurable and increase the default**: Increase the default staleness threshold from 5 minutes to a value that accommodates long-running operations (e.g., 15 minutes), and make it configurable via environment variable or config file.

4. **Remove the process-based fallback detection**: Remove `analyzeClaudeState`, `capturePaneContent`, `getProcessState`, and related process-inspection code from `internal/tmux/tmux.go`. When the state file is missing or stale, return `unknown` rather than attempting unreliable process detection.

5. **Validate all hooks are registered**: Change `isSetupInFile` to verify that ALL required hook events are registered, not just one. Provide a clear error or warning when hooks are partially registered.

6. **Add a session marker file**: At session creation time, write a marker file (e.g., `~/.tmux-claude-matrix/sessions/{sessionName}.env`) containing the `TMUX_PANE` and session name. The hook handler can read this as a fallback when `TMUX_PANE` is not in the environment.

7. **Add debug logging**: Implement a structured logging system for the hook handler that writes to a log file (e.g., `~/.tmux-claude-matrix/logs/hooks.log`). Log every hook event received, every state transition, every `TMUX_PANE` resolution attempt, and every error. Enable via `CLAUDE_MATRIX_DEBUG=1` environment variable or config setting.

8. **Add a diagnostic command**: Add `claude-matrix status --debug` (or `claude-matrix diagnose`) that reports:
   - Whether hooks are registered in `~/.claude/settings.json` (all events, not just any)
   - The binary path in the hook commands and whether it resolves to an executable
   - Current state files and their ages
   - Active tmux sessions and their window names
   - The `TMUX_PANE` value in the current environment

9. **Handle `SessionEnd` cleanup**: When a `SessionEnd` event fires, clean up the state file and reset the tmux window name. Ensure this works even if the session was in Teams mode and sub-agents have already exited.

10. **Log hook handler errors to stderr**: In addition to the debug log file, the hook handler should write errors to stderr so they appear in Claude Code's hook error output (if any), rather than silently returning nil.

## Acceptance Criteria

- [ ] Status correctly reflects `running`, `idle`, `waiting_for_input`, and `stopped` states in single-agent sessions
- [ ] Status correctly reflects states in Claude Code Teams sessions (multi-agent)
- [ ] When `TMUX_PANE` is not set (Teams sub-agent context), the hook handler still resolves the correct session and updates status
- [ ] Idempotency check no longer suppresses timestamp updates — staleness is based on last hook event, not last state change
- [ ] State file staleness threshold is configurable (default increased from 5 minutes)
- [ ] Process-based fallback detection (`analyzeClaudeState` and related code) is removed
- [ ] `isSetupInFile` validates ALL required hook events are registered, not just the first match
- [ ] `claude-matrix diagnose` command exists and reports hook registration status, binary path validity, state file ages, and environment
- [ ] Debug logging can be enabled via `CLAUDE_MATRIX_DEBUG=1` and writes to `~/.tmux-claude-matrix/logs/hooks.log`
- [ ] Hook handler errors are written to stderr (not silently swallowed)
- [ ] All existing tests pass; new tests cover TMUX_PANE resolution fallback, staleness threshold configuration, and the diagnostic command
- [ ] No regressions in session creation, listing, or deletion workflows

## Out of Scope

- Polling/process-based status detection as a fallback mechanism (explicitly removed)
- UI/UX changes to the FZF selection interface
- Changes to the tmux status bar format or emoji set
- Support for non-tmux terminal multiplexers
- Real-time status push via tmux hooks or inotify/fswatch
- Changes to how sessions are created or destroyed
- Performance optimization of hook handler execution
