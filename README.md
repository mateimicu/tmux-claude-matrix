# Tmux Claude Matrix

A tmux session manager with Claude AI integration.

## Installation

```bash
git clone https://github.com/mateimicu/tmux-claude-matrix.git
cd tmux-claude-matrix
./install.sh
```

Or as a tmux plugin via TPM:

```tmux
set -g @plugin 'mateimicu/tmux-claude-matrix'
```

## Usage

```bash
# Create a session
claude-matrix create

# List sessions
claude-matrix list

# Rename a session
claude-matrix rename [title]

# Refresh repository cache
claude-matrix refresh

# Check configuration
claude-matrix diagnose
```

## Features

<details>
<summary>Session Management</summary>

Create, list, and rename tmux sessions tied to repository clones. Sessions track metadata (repo URL, clone path, timestamps) and auto-generate unique names. Switch between sessions directly from the FZF list view.

</details>

<details>
<summary>Repository Discovery</summary>

Three repository sources:
- **Local file** (`repos.txt`) — list repos with optional descriptions
- **GitHub** — auto-discover repos from authenticated GitHub accounts, filterable by org
- **Workspaces** (`workspaces.yaml`) — group multiple repos into named workspaces

Results are cached with a 30-minute TTL. Supports HTTPS and SSH URL formats.

</details>

<details>
<summary>Claude AI Integration</summary>

Auto-detect and launch Claude in sessions. A hook system listens to Claude Code events (`SessionStart`, `UserPromptSubmit`, `PreToolUse`, `Stop`, `Notification`, `SessionEnd`) and tracks state in real time.

Six states with visual indicators in tmux windows and the session list:
- Running, Waiting for Input, Idle, Stopped, Error, Unknown

Setup with `claude-matrix setup-hooks`, remove with `claude-matrix remove-hooks`.

</details>

<details>
<summary>FZF Interactive UI</summary>

Interactive selection for both repository browsing and session management:
- Aligned table view with columns: index, tmux status, source, repository, Claude state, session name
- `Enter` to switch, `Ctrl+D` to delete
- Emoji legend in the header

</details>

<details>
<summary>Git Mirror Cache</summary>

Clones use a local mirror cache (`~/.tmux-claude-matrix/.cache/mirrors/`) so subsequent clones of the same repo are fast local operations instead of full network fetches.

</details>

<details>
<summary>Status Bar Integration</summary>

Sessions have auto-generated titles (`org/repo #N`) shown in the tmux status bar via the `@claude-matrix-title` environment variable. Use `claude-matrix rename` to customize titles.

```tmux
set -g status-right "#{@claude-matrix-title} | %H:%M"
```

</details>

<details>
<summary>Tmux Keybindings</summary>

When installed as a tmux plugin:
- `prefix + a` — create session
- `prefix + A` — list sessions

</details>

## Configuration

Create `~/.config/tmux-claude-matrix/config` (or `~/.tmux-claude-matrix/config`):

```bash
# Repository sources
GITHUB_ENABLED=1
LOCAL_CONFIG_ENABLED=1
LOCAL_REPOS_FILE=~/.tmux-claude-matrix/repos.txt
WORKSPACES_ENABLED=1
WORKSPACES_FILE=~/.tmux-claude-matrix/workspaces.yaml

# Directories
CLONE_DIR=~/.tmux-claude-matrix/repos
SESSIONS_DIR=~/.tmux-claude-matrix/sessions
CACHE_DIR=~/.tmux-claude-matrix/.cache

# Claude integration
CLAUDE_BIN=/usr/local/bin/claude
CLAUDE_ARGS="--dangerously-skip-permissions"

# GitHub filtering
GITHUB_ORGS=org1,org2
```

All options can also be set via environment variables prefixed with `TMUX_CLAUDE_MATRIX_` (e.g. `TMUX_CLAUDE_MATRIX_CLONE_DIR`).

## License

MIT
