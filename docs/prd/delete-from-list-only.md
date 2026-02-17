# PRD: Consolidate Session Deletion to List View Only

## Goal

Remove the standalone `delete` subcommand reference and the `prefix + D` tmux keybinding, making the list view (`claude-matrix list` with `Ctrl+D`) the single entry point for session deletion. This simplifies the user experience by eliminating redundant (and currently non-functional) delete paths.

## Background

The codebase currently has three documented deletion paths:

1. **`claude-matrix delete [session-name]`** -- Documented in the README but never implemented as a CLI subcommand. Running it produces an "unknown command" error.
2. **`prefix + D` tmux keybinding** -- Defined in `claude-matrix.tmux` (line 161), bound to `$BINARY delete` which fails because the subcommand doesn't exist.
3. **`Ctrl+D` in list view** -- Fully implemented in `internal/fzf/fzf.go` and `cmd/claude-matrix/list.go`. This is the only working delete path.

Since the list view delete (`Ctrl+D`) is the only functional path and provides a superior UX (confirmation prompt, visual context of which session is selected), the standalone delete subcommand and dedicated keybinding should be removed rather than implemented.

## Requirements

1. Remove the `prefix + D` keybinding from `claude-matrix.tmux`:
   - Remove the `delete_key` variable and its `get_tmux_option` call for `@claude-matrix-delete-key`
   - Remove the `tmux bind-key "$delete_key"` lines from both popup and non-popup branches in `bind_keys()`
2. Update the README to remove references to the non-existent delete subcommand:
   - Remove `claude-matrix delete [session-name]` from the Usage code block
   - Remove `prefix + D -- delete session` from the Tmux Keybindings section
   - Mention `Ctrl+D` in the FZF Interactive UI section as the delete mechanism (already documented there)
3. Ensure the existing list-view delete functionality (`Ctrl+D` in FZF) remains unchanged:
   - `handleDeleteAction` in `cmd/claude-matrix/list.go` continues to work as-is
   - Confirmation prompt, tmux session kill, metadata deletion, and status cleanup are preserved

## Acceptance Criteria

- [ ] `claude-matrix.tmux` no longer references a delete keybinding or `@claude-matrix-delete-key` option
- [ ] README does not document `claude-matrix delete` as a subcommand
- [ ] README does not document `prefix + D` as a keybinding
- [ ] `Ctrl+D` in the list view (`claude-matrix list`) continues to delete sessions with confirmation
- [ ] `make check` (lint + tests) passes with no regressions
- [ ] No new subcommands are added (no `delete.go`)

## Out of Scope

- Adding a new standalone `delete` subcommand (the decision is to not implement it)
- Changing the delete confirmation UX in the list view
- Cleaning up cloned repository directories on disk (clone cleanup is a separate concern)
- Adding batch/multi-select delete support in the list view
