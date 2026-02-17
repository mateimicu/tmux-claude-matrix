# Implementation Spec: Consolidate Session Deletion to List View Only

**PRD:** `docs/prd/delete-from-list-only.md`
**Type:** Removal / Simplification
**Scope:** 2 files changed, 0 files added, 0 files removed

## Context

The codebase has three documented deletion paths, but only one works — `Ctrl+D` in the FZF list view. The other two (a `prefix + D` tmux keybinding and a `claude-matrix delete` CLI subcommand reference) are dead paths: the keybinding calls a subcommand that was never implemented, and the README documents it as if it exists.

This spec removes the dead references. No Go code changes are needed — the working delete path lives entirely in `cmd/claude-matrix/list.go` and `internal/fzf/fzf.go`, both of which remain untouched.

## Architecture Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Remove vs implement `delete` subcommand | Remove references | PRD decision: list-view delete provides superior UX with visual context and confirmation |
| Remove vs keep `prefix + D` keybinding | Remove | Keybinding calls non-existent `$BINARY delete`; broken since inception |
| Scope of Go changes | None | All working delete logic (`handleDeleteAction`, `SessionActionDelete`, FZF key handling) is already correct and stays as-is |

## Component Structure & Interfaces

No new components, interfaces, or data structures are introduced. No existing Go interfaces change.

### Files Changed

#### 1. `claude-matrix.tmux` — Remove delete keybinding

**What changes in `bind_keys()` (lines 157–173):**

- Remove the `delete_key` variable declaration (line 158 local list, line 161 assignment)
- Remove the `tmux bind-key "$delete_key"` line in the popup branch (line 167)
- Remove the `tmux bind-key "$delete_key"` line in the non-popup branch (line 171)

**After the change, `bind_keys()` should only bind `create_key` and `list_key`.** The `@claude-matrix-delete-key` tmux option becomes unused and is no longer read.

#### 2. `README.md` — Remove dead documentation

Three changes:

1. **Usage code block (lines 27–29):** Remove the `# Delete a session` / `claude-matrix delete [session-name]` lines.
2. **Session Management summary (line 46):** Change "Create, list, delete, and rename" to "Create, list, and rename" — the word "delete" here implies a standalone operation; session deletion is part of the list view and already documented in the FZF section.
3. **Tmux Keybindings section (lines 105–110):** Remove `prefix + D — delete session` line. Only `prefix + a` (create) and `prefix + A` (list) remain.

### Files NOT Changed (verification scope)

These files contain the working delete path and must remain untouched:

- `cmd/claude-matrix/list.go` — `handleDeleteAction()` stays as-is
- `internal/fzf/fzf.go` — `SessionActionDelete`, `SelectSessionWithAction()`, key bindings (`ctrl-d`) stay as-is
- `internal/session/session.go` — `Manager.Delete()` stays as-is
- `internal/status/status.go` — `RemoveState()`, `RemoveAllAgentStates()` stay as-is
- `internal/tmux/tmux.go` — `KillSession()` stays as-is
- `pkg/types/types.go` — No changes

## Integration Points

No integration points change. The delete data flow is entirely within the list command:

```
FZF UI (ctrl-d) → SessionActionDelete → handleDeleteAction() → session.Delete() + tmux.KillSession() + status.Remove*()
```

This flow is unaffected by the removal of the tmux keybinding and README text.

## Data Flow

No data flow changes. The only modification is removing a broken entry point (`prefix + D` → `$BINARY delete` → command not found error). The working entry point (`claude-matrix list` → FZF → `Ctrl+D`) is unchanged.

## Test Strategy

### Existing Tests (must continue passing)

- `internal/fzf/fzf_test.go` — `TestSessionActions_NoDuplicateValues` (guard test for SessionAction enum)
- `internal/session/session_test.go` — Delete test in `TestSessionManager`
- All other tests via `make check`

### New Tests

None required. This is a pure removal of dead code paths (shell script lines and documentation). The removed keybinding never had tests (it's a tmux plugin script, not Go code), and the README changes are documentation-only.

### Verification

- `make check` (lint + test) passes with no regressions
- Manual: `grep -r 'delete' claude-matrix.tmux` returns no matches
- Manual: README does not mention `claude-matrix delete` or `prefix + D`

## Coding Expert Assignment

**Single coding expert** — this is a small, self-contained removal task.

### Scope

| File | Type of Change | Estimated Lines Changed |
|------|---------------|------------------------|
| `claude-matrix.tmux` | Remove 4 lines (delete_key var + 2 bind-key lines + local var reference) | ~4 lines removed |
| `README.md` | Remove 3 lines, edit 1 line | ~3 lines removed, ~1 line edited |

### Instructions for Coding Expert

1. Edit `claude-matrix.tmux` `bind_keys()`:
   - Remove `delete_key` from the `local` declaration on line 158
   - Remove the `delete_key=...` assignment (line 161)
   - Remove `tmux bind-key "$delete_key" display-popup ...` (line 167)
   - Remove `tmux bind-key "$delete_key" new-window ...` (line 171)
2. Edit `README.md`:
   - Remove lines 27–29 (`# Delete a session` block)
   - Edit line 46: change "Create, list, delete, and rename" to "Create, list, and rename"
   - Remove line 108 (`prefix + D — delete session`)
3. Run `make check` to verify no regressions
4. Do NOT create any new Go files, subcommands, or tests

## Acceptance Criteria

- [ ] `claude-matrix.tmux` `bind_keys()` no longer references `delete_key`, `@claude-matrix-delete-key`, or `$BINARY delete`
- [ ] README does not document `claude-matrix delete` as a subcommand
- [ ] README does not document `prefix + D` as a keybinding
- [ ] `Ctrl+D` in the list view continues to work (no Go code changed)
- [ ] `make check` passes with no regressions
- [ ] No new files added
