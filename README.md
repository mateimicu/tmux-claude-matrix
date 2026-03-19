# Tmux Claude Matrix

A Go CLI tool for managing tmux development sessions with Claude AI state tracking.

## Quick Start

```bash
# Install
git clone https://github.com/mateimicu/tmux-claude-matrix.git
cd tmux-claude-matrix
make build        # binary at ./bin/claude-matrix
./install.sh      # or install as tmux plugin
```

Or via TPM: `set -g @plugin 'mateimicu/tmux-claude-matrix'`

## Commands

| Command | Description |
|---------|-------------|
| `create` | Create a new session from a repo |
| `list` | List and switch between sessions |
| `rename` | Rename a session title |
| `refresh` | Refresh repository cache |
| `diagnose` | Check configuration |
| `setup-hooks` | Enable Claude state tracking |
| `remove-hooks` | Disable Claude state tracking |

## Key Features

- **Session management** — tmux sessions tied to repo clones with metadata tracking
- **Repo discovery** — GitHub API, local file (`repos.txt`), or YAML workspaces
- **Claude integration** — hooks track Claude Code state (running, idle, waiting, stopped, error) in real time
- **FZF UI** — interactive selection with `Enter` to switch, `Ctrl+D` to delete
- **Mirror cache** — fast re-clones via local git mirrors

## Configuration

Config file at `~/.config/tmux-claude-matrix/config` or `~/.tmux-claude-matrix/config`. All options also available as `TMUX_CLAUDE_MATRIX_`-prefixed env vars.

Key settings: `GITHUB_ENABLED`, `CLONE_DIR`, `CLAUDE_BIN`, `CLAUDE_ARGS`, `GITHUB_ORGS`.

See `claude-matrix diagnose` for current config status.

## Development

```bash
make test    # tests with race detector
make lint    # golangci-lint
make ci      # full CI: fmt + lint + test + build
```

## License

MIT
