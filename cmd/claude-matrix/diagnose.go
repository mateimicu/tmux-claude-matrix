package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mateimicu/tmux-claude-matrix/internal/config"
	"github.com/mateimicu/tmux-claude-matrix/internal/hooks"
	"github.com/mateimicu/tmux-claude-matrix/internal/repos"
	"github.com/mateimicu/tmux-claude-matrix/internal/status"
	"github.com/mateimicu/tmux-claude-matrix/internal/tmux"
)

func diagnoseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diagnose",
		Short: "Diagnose repository discovery issues",
		Long:  `Show configuration and test repository sources.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiagnose(cmd.Context())
		},
	}
}

func runDiagnose(ctx context.Context) error {
	fmt.Println("🔍 Diagnosing tmux-claude-matrix configuration...")
	fmt.Println()

	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("❌ Failed to load config: %w", err)
	}

	fmt.Println("✓ Configuration loaded successfully")
	fmt.Println()

	// --- New diagnostic sections (hooks, state, sessions, env) ---

	diagnoseHookRegistration()
	diagnoseStateFiles(cfg.StaleThreshold)
	diagnoseTmuxSessions()
	diagnoseEnvironment(cfg.StaleThreshold)

	// --- Existing repository diagnostic sections ---

	// Show configuration
	fmt.Println("📋 Configuration:")
	fmt.Printf("  Clone directory: %s\n", cfg.CloneDir)
	fmt.Printf("  Sessions directory: %s\n", cfg.SessionsDir)
	fmt.Printf("  Cache directory: %s\n", cfg.CacheDir)
	fmt.Printf("  Cache TTL: %s\n", cfg.CacheTTL)
	fmt.Println()

	// Check local repos
	fmt.Println("📁 Local Repository Source:")
	fmt.Printf("  Enabled: %v\n", cfg.LocalConfigEnabled)
	if cfg.LocalConfigEnabled {
		fmt.Printf("  File: %s\n", cfg.LocalReposFile)

		// Check if file exists
		if _, err := os.Stat(cfg.LocalReposFile); err == nil {
			fmt.Println("  Status: ✓ File exists")

			// Try to load repos
			source := repos.NewLocalSource(cfg.LocalReposFile)
			localRepos, err := source.List(ctx)
			if err != nil {
				fmt.Printf("  Error: ❌ %v\n", err)
			} else {
				fmt.Printf("  Repositories found: %d\n", len(localRepos))
				for i, repo := range localRepos {
					if i < 5 { // Show first 5
						fmt.Printf("    - %s\n", repo.Name)
					}
				}
				if len(localRepos) > 5 {
					fmt.Printf("    ... and %d more\n", len(localRepos)-5)
				}
			}
		} else {
			fmt.Println("  Status: ❌ File not found")
			fmt.Printf("  Create it with: echo 'https://github.com/user/repo' > %s\n", cfg.LocalReposFile)
		}
	} else {
		fmt.Println("  Status: Disabled")
	}
	fmt.Println()

	// Check GitHub
	fmt.Println("🐙 GitHub Repository Source:")
	fmt.Printf("  Enabled: %v\n", cfg.GitHubEnabled)

	var ghToken string
	var ghTokenSource string

	if cfg.GitHubEnabled {
		ghToken, ghTokenSource = repos.GetGitHubToken(ctx)
		if ghToken == "" {
			fmt.Println("  Status: ❌ No GitHub authentication found")
			fmt.Println()
			fmt.Println("  To enable GitHub integration:")
			fmt.Println("    Option 1: Use gh CLI (recommended)")
			fmt.Println("      - Install: brew install gh  (macOS)")
			fmt.Println("      - Login: gh auth login")
			fmt.Println("      - Verify: gh auth status")
			fmt.Println()
			fmt.Println("    Option 2: Use token manually")
			fmt.Println("      - Get token: https://github.com/settings/tokens")
			fmt.Println("      - Export: export GITHUB_TOKEN=\"ghp_your_token\"")
			fmt.Println("      - Or run: ./setup-github.sh")
		} else {
			fmt.Printf("  Authentication: ✓ Using %s\n", ghTokenSource)
			fmt.Printf("  Token: %s...\n", ghToken[:10])

			// Try to fetch repos
			fmt.Println("  Testing GitHub API...")
			if len(cfg.GitHubOrgs) > 0 {
				fmt.Printf("  Organization filter: %s\n", strings.Join(cfg.GitHubOrgs, ", "))
			}
			source := repos.NewGitHubSource(ghToken, cfg.CacheDir, cfg.CacheTTL, cfg.GitHubOrgs)
			githubRepos, err := source.List(ctx)
			if err != nil {
				fmt.Printf("  Error: ❌ %v\n", err)
				fmt.Println()
				fmt.Println("  Common issues:")
				fmt.Println("    - Token expired or invalid")
				fmt.Println("    - Token missing 'repo' scope")
				fmt.Println("    - Network connectivity issues")
			} else {
				fmt.Printf("  Status: ✓ API working\n")
				fmt.Printf("  Repositories found: %d\n", len(githubRepos))
				for i, repo := range githubRepos {
					if i < 5 { // Show first 5
						fmt.Printf("    - %s\n", repo.Name)
					}
				}
				if len(githubRepos) > 5 {
					fmt.Printf("    ... and %d more\n", len(githubRepos)-5)
				}
			}
		}
	} else {
		fmt.Println("  Status: Disabled")
	}
	fmt.Println()

	// Summary
	fmt.Println("📊 Summary:")

	// Count total sources
	var sources []repos.Source
	if cfg.LocalConfigEnabled && cfg.LocalReposFile != "" {
		sources = append(sources, repos.NewLocalSource(cfg.LocalReposFile))
	}
	if cfg.GitHubEnabled && ghToken != "" {
		sources = append(sources, repos.NewGitHubSource(ghToken, cfg.CacheDir, cfg.CacheTTL, cfg.GitHubOrgs))
	}

	if len(sources) == 0 {
		fmt.Println("  ❌ No repository sources configured!")
		fmt.Println()
		fmt.Println("  To fix:")
		fmt.Println("    1. Add local repos: echo 'https://github.com/user/repo' > ~/.tmux-claude-matrix/repos.txt")
		fmt.Println("    2. Or set GITHUB_TOKEN: ./setup-github.sh")
	} else {
		discoverer := repos.NewDiscoverer(sources...)
		discoveryCtx, discoveryCancel := context.WithTimeout(ctx, 15*time.Second)
		defer discoveryCancel()
		allRepos, err := discoverer.ListAll(discoveryCtx)
		if err != nil {
			fmt.Printf("  Error discovering repos: %v\n", err)
		} else {
			fmt.Printf("  ✓ Total repositories available: %d\n", len(allRepos))
		}
	}

	fmt.Println()
	fmt.Println("For more help, see: https://github.com/mateimicu/tmux-claude-matrix")

	return nil
}

// diagnoseHookRegistration checks hook registration status.
func diagnoseHookRegistration() {
	fmt.Println("🪝 Hook Registration:")

	// Find binary path
	binaryPath, err := os.Executable()
	if err != nil {
		fmt.Printf("  ❌ Could not determine binary path: %v\n", err)
		fmt.Println()
		return
	}

	fmt.Printf("  Binary: %s\n", binaryPath)
	if _, err := os.Stat(binaryPath); err != nil {
		fmt.Printf("  ❌ Binary not found at path\n")
	} else {
		fmt.Printf("  ✓ Binary exists\n")
	}

	missing, err := hooks.MissingHookEvents(binaryPath)
	if err != nil {
		fmt.Printf("  ❌ Error checking hooks: %v\n", err)
		fmt.Println()
		return
	}

	if len(missing) == 0 {
		fmt.Println("  ✓ All hook events registered")
	} else {
		fmt.Printf("  ❌ Missing hook events: %s\n", strings.Join(missing, ", "))
		fmt.Println("  Fix with: claude-matrix setup-hooks")
	}
	fmt.Println()
}

// diagnoseStateFiles lists state files and their per-agent entries.
func diagnoseStateFiles(staleThreshold time.Duration) {
	fmt.Println("📄 State Files:")

	statusDir := status.DefaultStatusDir()
	entries, err := os.ReadDir(statusDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("  No state directory found (no active sessions)")
		} else {
			fmt.Printf("  ❌ Error reading status dir: %v\n", err)
		}
		fmt.Println()
		return
	}

	stateFiles := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".state") {
			stateFiles++
			sessionName := strings.TrimSuffix(entry.Name(), ".state")
			fmt.Printf("  Session: %s\n", sessionName)

			sf, err := status.ReadStateFile(statusDir, sessionName)
			if err != nil {
				fmt.Printf("    ❌ Error reading: %v\n", err)
				continue
			}

			for agentID, agent := range sf.Agents {
				age := time.Since(agent.UpdatedAt).Truncate(time.Second)
				stale := ""
				if time.Since(agent.UpdatedAt) > staleThreshold {
					stale = " (STALE)"
				}
				fmt.Printf("    Agent %s: state=%s, age=%s%s\n", agentID, agent.State, age, stale)
			}

			aggregate, _ := status.ComputeState(sf, staleThreshold)
			fmt.Printf("    Aggregate: %s %s\n", status.EmojiForState(aggregate), aggregate)
		}
	}

	if stateFiles == 0 {
		fmt.Println("  No state files found")
	}
	fmt.Println()
}

// diagnoseTmuxSessions lists active tmux sessions.
func diagnoseTmuxSessions() {
	fmt.Println("🖥️  Active Tmux Sessions:")

	mgr := tmux.New()
	sessions, err := mgr.ListSessions()
	if err != nil {
		fmt.Printf("  ❌ Error listing sessions: %v\n", err)
		fmt.Println()
		return
	}

	if len(sessions) == 0 {
		fmt.Println("  No active tmux sessions")
	} else {
		for _, name := range sessions {
			fmt.Printf("  - %s\n", name)
		}
	}
	fmt.Println()
}

// diagnoseEnvironment shows relevant environment variables.
func diagnoseEnvironment(staleThreshold time.Duration) {
	fmt.Println("🔧 Environment:")
	fmt.Printf("  TMUX_PANE: %s\n", envOrEmpty("TMUX_PANE"))
	fmt.Printf("  CLAUDE_MATRIX_DEBUG: %s\n", envOrEmpty("CLAUDE_MATRIX_DEBUG"))
	fmt.Printf("  CLAUDE_MATRIX_STALE_THRESHOLD: %s\n", envOrEmpty("CLAUDE_MATRIX_STALE_THRESHOLD"))
	fmt.Printf("  Configured stale threshold: %s\n", staleThreshold)

	settingsPath := filepath.Join(os.Getenv("HOME"), ".claude/settings.json")
	if _, err := os.Stat(settingsPath); err == nil {
		fmt.Printf("  Claude settings file: ✓ %s\n", settingsPath)
	} else {
		fmt.Printf("  Claude settings file: ❌ not found at %s\n", settingsPath)
	}
	fmt.Println()
}

func envOrEmpty(key string) string {
	val := os.Getenv(key)
	if val == "" {
		return "(not set)"
	}
	return val
}
