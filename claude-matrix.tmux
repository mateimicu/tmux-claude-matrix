#!/usr/bin/env bash
# Tmux Claude Matrix Plugin
# Entry point for tmux plugin manager

CURRENT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="$CURRENT_DIR/bin/claude-matrix"

# Get tmux option value
get_tmux_option() {
    local option="$1"
    local default_value="$2"
    local option_value
    option_value=$(tmux show-option -gqv "$option")
    if [ -z "$option_value" ]; then
        echo "$default_value"
    else
        echo "$option_value"
    fi
}

# Detect OS and architecture
detect_platform() {
    local os arch
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    arch="$(uname -m)"

    case "$os" in
        darwin|linux) ;;
        *) echo ""; return ;;
    esac

    case "$arch" in
        x86_64|amd64) arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        *) echo ""; return ;;
    esac

    echo "${os}-${arch}"
}

# Verify checksum of downloaded binary
verify_checksum() {
    local file="$1"
    local checksums_file="$2"
    local filename
    filename="$(basename "$file")"

    if [ ! -f "$checksums_file" ]; then
        return 1
    fi

    local expected
    expected="$(grep "$filename" "$checksums_file" | awk '{print $1}')"
    if [ -z "$expected" ]; then
        return 1
    fi

    local actual
    if command -v sha256sum >/dev/null 2>&1; then
        actual="$(sha256sum "$file" | awk '{print $1}')"
    elif command -v shasum >/dev/null 2>&1; then
        actual="$(shasum -a 256 "$file" | awk '{print $1}')"
    else
        return 1
    fi

    [ "$expected" = "$actual" ]
}

# Query GitHub API for the latest release tag
get_latest_tag() {
    local repo="$1"
    local api_url="https://api.github.com/repos/${repo}/releases/latest"
    local release_json

    if command -v curl >/dev/null 2>&1; then
        release_json="$(curl -sf --connect-timeout 10 "$api_url")"
    elif command -v wget >/dev/null 2>&1; then
        release_json="$(wget -qO- --timeout=10 "$api_url")"
    else
        return 1
    fi

    if [ -z "$release_json" ]; then
        return 1
    fi

    # Extract tag name (works without jq)
    local tag
    tag="$(echo "$release_json" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')"
    if [ -z "$tag" ]; then
        return 1
    fi

    echo "$tag"
}

# Download pre-built binary from GitHub Releases for a specific tag
download_binary() {
    local repo="$1"
    local platform="$2"
    local dest="$3"
    local tag="$4"

    local binary_name="claude-matrix-${platform}"
    local base_url="https://github.com/${repo}/releases/download/${tag}"
    local binary_url="${base_url}/${binary_name}"
    local checksums_url="${base_url}/checksums.txt"

    local tmpdir
    tmpdir="$(mktemp -d)"
    trap 'rm -rf "$tmpdir"' RETURN

    # Download binary and checksums
    if command -v curl >/dev/null 2>&1; then
        curl -sfL --connect-timeout 10 -o "$tmpdir/$binary_name" "$binary_url" || return 1
        curl -sfL --connect-timeout 10 -o "$tmpdir/checksums.txt" "$checksums_url" || return 1
    elif command -v wget >/dev/null 2>&1; then
        wget -q --timeout=10 -O "$tmpdir/$binary_name" "$binary_url" || return 1
        wget -q --timeout=10 -O "$tmpdir/checksums.txt" "$checksums_url" || return 1
    else
        return 1
    fi

    # Verify checksum
    if ! verify_checksum "$tmpdir/$binary_name" "$tmpdir/checksums.txt"; then
        return 1
    fi

    # Install binary (stage in target dir for atomic same-filesystem rename)
    local dest_dir
    dest_dir="$(dirname "$dest")"
    mkdir -p "$dest_dir"
    mv "$tmpdir/$binary_name" "${dest}.tmp"
    mv "${dest}.tmp" "$dest"
    chmod +x "$dest"
}

# Build from source using make
build_from_source() {
    local src_dir="$1"

    if ! command -v go >/dev/null 2>&1; then
        return 1
    fi

    if [ ! -f "$src_dir/go.mod" ]; then
        return 1
    fi

    (cd "$src_dir" && make build >/dev/null 2>&1)
}

# --- Main ---

# Helper: bind keybindings for the plugin
bind_keys() {
    local create_key list_key use_popup
    create_key=$(get_tmux_option "@claude-matrix-create-key" "a")
    list_key=$(get_tmux_option "@claude-matrix-list-key" "A")
    use_popup=$(get_tmux_option "@claude-matrix-use-popup" "true")

    if [ "$use_popup" = "true" ]; then
        tmux bind-key "$create_key" display-popup -w 80% -h 80% -E "$BINARY create"
        tmux bind-key "$list_key" display-popup -w 80% -h 80% -E "$BINARY list"
    else
        tmux bind-key "$create_key" new-window "$BINARY create"
        tmux bind-key "$list_key" new-window "$BINARY list"
    fi
}

# Determine what action is needed
repo=$(get_tmux_option "@claude-matrix-repo" "mateimicu/tmux-claude-matrix")
platform="$(detect_platform)"

if [ ! -x "$BINARY" ]; then
    # No binary — install synchronously so key bindings point to a real binary.
    # A background install would race: keys would be bound before the binary exists.
    local_tag=""
    if [ -n "$platform" ]; then
        local_tag="$(get_latest_tag "$repo")"
    fi

    if [ -n "$local_tag" ] && download_binary "$repo" "$platform" "$BINARY" "$local_tag"; then
        tmux display-message "claude-matrix: Installed pre-built binary ($local_tag)"
    elif build_from_source "$CURRENT_DIR"; then
        tmux display-message "claude-matrix: Built from source"
    else
        tmux display-message "claude-matrix: Install failed. Download from GitHub releases or install Go and run 'make build'."
    fi

    # Bind keys only if installation succeeded
    if [ -x "$BINARY" ]; then
        bind_keys
    fi
else
    # Binary exists — bind keys immediately (existing binary works), then
    # check for updates in the background. The atomic mv in download_binary
    # ensures a key press mid-update won't hit a partial file.
    bind_keys

    (
        # Primary: version-based staleness check against latest release
        if [ -n "$platform" ]; then
            installed_version="$("$BINARY" version 2>/dev/null | awk '{print $2}')"
            latest_tag="$(get_latest_tag "$repo" 2>/dev/null)"

            if [ -n "$latest_tag" ] && [ "$installed_version" != "$latest_tag" ]; then
                if download_binary "$repo" "$platform" "$BINARY" "$latest_tag"; then
                    tmux display-message "claude-matrix: Updated $installed_version -> $latest_tag"
                    exit 0
                fi
            fi
        fi

        # Secondary: source-file timestamp check (development workflow)
        if [ -n "$(find "$CURRENT_DIR" -not -path '*/vendor/*' -not -path '*/.git/*' -name '*.go' -newer "$BINARY" -print -quit 2>/dev/null)" ] || \
           [ "$CURRENT_DIR/go.mod" -nt "$BINARY" ] || \
           [ "$CURRENT_DIR/go.sum" -nt "$BINARY" ]; then
            if build_from_source "$CURRENT_DIR"; then
                tmux display-message "claude-matrix: Rebuilt from source"
                exit 0
            fi
        fi
    ) &
fi

# Session titles: each session sets @claude-matrix-title as a session-level
# environment variable. Add #{@claude-matrix-title} to status-right or
# status-left to display the current session's title in the tmux status bar.
