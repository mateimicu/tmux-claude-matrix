package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mateimicu/tmux-claude-matrix/internal/fzf"
	"github.com/mateimicu/tmux-claude-matrix/internal/git"
	"github.com/mateimicu/tmux-claude-matrix/internal/logging"
	"github.com/mateimicu/tmux-claude-matrix/internal/repos"
	"github.com/mateimicu/tmux-claude-matrix/internal/session"
	"github.com/mateimicu/tmux-claude-matrix/internal/tmux"
	"github.com/mateimicu/tmux-claude-matrix/pkg/types"
)

func createCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create",
		Short: "Create a new tmux session",
		Long:  `Create a new tmux session by selecting a repository from configured sources.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd.Context())
		},
	}
}

func runCreate(ctx context.Context) error {
	cfg := configFromContext(ctx)
	log := loggerFromContext(ctx)

	// Build sources list
	sources, err := buildSources(ctx, cfg, log)
	if err != nil {
		return err
	}

	// Discover repos
	discoverer := repos.NewDiscoverer(sources...)
	log.Debugf("🔍 Discovering repositories...\n")

	discoveryCtx, discoveryCancel := context.WithTimeout(ctx, 15*time.Second)
	defer discoveryCancel()

	repoList, err := discoverer.ListAll(discoveryCtx)
	if err != nil {
		return fmt.Errorf("failed to discover repositories: %w", err)
	}

	if len(repoList) == 0 {
		return fmt.Errorf("no repositories found")
	}

	log.Debugf("✓ Found %d repositories\n", len(repoList))

	// Get binary path for FZF reload
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get binary path: %w", err)
	}

	// Let user select
	selected, err := fzf.SelectRepository(repoList, binaryPath)
	if err != nil {
		return fmt.Errorf("repository selection cancelled: %w", err)
	}

	sessionMgr := session.NewManager(cfg.SessionsDir)
	gitMgr := git.New()
	tmuxMgr := tmux.New()

	if selected.IsWorkspace {
		return createWorkspaceSession(cfg, selected, sessionMgr, gitMgr, tmuxMgr, log)
	}

	return createRepoSession(cfg, selected, sessionMgr, gitMgr, tmuxMgr, log)
}

// buildClaudeWorktreeCmd builds the command string for running Claude Code
// with --worktree, so Claude manages worktree creation/cleanup itself.
func buildClaudeWorktreeCmd(cfg *types.Config, worktreeName string) string {
	if cfg.ClaudeBin == "" {
		return ""
	}
	args := append([]string{cfg.ClaudeBin, "--worktree", worktreeName}, cfg.ClaudeArgs...)
	return strings.Join(args, " ")
}

func createRepoSession(cfg *types.Config, selected *types.Repository, sessionMgr *session.Manager, gitMgr *git.Manager, tmuxMgr *tmux.Manager, log *logging.Logger) error {
	repoName := git.ExtractRepoName(selected.URL)
	sessionName, err := sessionMgr.GenerateUniqueName(repoName)
	if err != nil {
		return fmt.Errorf("failed to generate session name: %w", err)
	}

	// Ensure we have a base clone of the repo
	log.Debugf("📦 Ensuring base repo for %s...\n", selected.URL)
	baseRepoPath, err := gitMgr.EnsureBaseRepo(selected.URL, cfg.BaseRepoDir)
	if err != nil {
		return fmt.Errorf("failed to ensure base repo: %w", err)
	}
	log.Debugf("✓ Base repo ready at %s\n", baseRepoPath)

	sess := &types.Session{
		Name:         sessionName,
		RepoURL:      selected.URL,
		Title:        sessionName,
		BaseRepoPath: baseRepoPath,
		CreatedAt:    time.Now(),
	}

	// Launch claude --worktree inside tmux; Claude Code creates and manages the worktree
	claudeCmd := buildClaudeWorktreeCmd(cfg, sessionName)
	return finalizeSession(sess, baseRepoPath, claudeCmd, sessionMgr, tmuxMgr, log)
}

func createWorkspaceSession(cfg *types.Config, selected *types.Repository, sessionMgr *session.Manager, gitMgr *git.Manager, tmuxMgr *tmux.Manager, log *logging.Logger) error {
	sessionName, err := sessionMgr.GenerateUniqueName(selected.Name)
	if err != nil {
		return fmt.Errorf("failed to generate session name: %w", err)
	}

	log.Debugf("📦 Setting up workspace '%s' with %d repos...\n", selected.Name, len(selected.WorkspaceRepos))

	// Ensure base clones exist for all repos and collect paths
	type repoInfo struct {
		name     string
		basePath string
	}
	var repoInfos []repoInfo
	for _, repoURL := range selected.WorkspaceRepos {
		name := git.ExtractRepoName(repoURL)
		log.Debugf("  📦 Ensuring base repo for %s...\n", name)
		basePath, err := gitMgr.EnsureBaseRepo(repoURL, cfg.BaseRepoDir)
		if err != nil {
			return fmt.Errorf("failed to ensure base repo for %s: %w", repoURL, err)
		}
		repoInfos = append(repoInfos, repoInfo{name: name, basePath: basePath})
		log.Debugf("  ✓ %s ready\n", name)
	}

	// First repo gets the main tmux session window
	first := repoInfos[0]
	worktreeName := sessionName + "-" + strings.ReplaceAll(first.name, "/", "-")
	claudeCmd := buildClaudeWorktreeCmd(cfg, worktreeName)

	log.Debugf("🚀 Creating tmux session '%s'...\n", sessionName)
	if err := tmuxMgr.CreateSession(sessionName, first.basePath, claudeCmd); err != nil {
		return fmt.Errorf("failed to create tmux session: %w", err)
	}

	// Remaining repos each get a new tmux window
	for _, ri := range repoInfos[1:] {
		wName := sessionName + "-" + strings.ReplaceAll(ri.name, "/", "-")
		cmd := buildClaudeWorktreeCmd(cfg, wName)
		if err := tmuxMgr.CreateWindow(sessionName, ri.name, cmd, ri.basePath); err != nil {
			log.Warnf("⚠️  Failed to create window for %s: %v\n", ri.name, err)
		}
	}

	sess := &types.Session{
		Name:      sessionName,
		RepoURL:   "workspace:" + selected.Name,
		Title:     sessionName,
		RepoURLs:  selected.WorkspaceRepos,
		CreatedAt: time.Now(),
	}
	if err := sessionMgr.Save(sess); err != nil {
		log.Warnf("⚠️  Failed to save session metadata: %v\n", err)
	}

	if err := tmuxMgr.SetSessionEnv(sessionName, "@claude-matrix-title", sessionName); err != nil {
		log.Warnf("⚠️  Failed to set session title env: %v\n", err)
	}

	fmt.Printf("✓ Workspace session created: %s (%d windows)\n", sessionName, len(repoInfos))

	if err := tmuxMgr.SwitchToSession(sessionName); err != nil {
		log.Warnf("⚠️  Failed to switch to session: %v\n", err)
		log.Warnf("You can attach manually with: tmux attach -t %s\n", sessionName)
	}

	return nil
}

// finalizeSession creates the tmux session, saves metadata, and switches to it.
func finalizeSession(sess *types.Session, workDir, claudeCmd string, sessionMgr *session.Manager, tmuxMgr *tmux.Manager, log *logging.Logger) error {
	log.Debugf("🚀 Creating tmux session '%s'...\n", sess.Name)
	if err := tmuxMgr.CreateSession(sess.Name, workDir, claudeCmd); err != nil {
		return fmt.Errorf("failed to create tmux session: %w", err)
	}

	if err := sessionMgr.Save(sess); err != nil {
		log.Warnf("⚠️  Failed to save session metadata: %v\n", err)
	}

	if err := tmuxMgr.SetSessionEnv(sess.Name, "@claude-matrix-title", sess.Name); err != nil {
		log.Warnf("⚠️  Failed to set session title env: %v\n", err)
	}

	fmt.Printf("✓ Session created: %s\n", sess.Name)

	if err := tmuxMgr.SwitchToSession(sess.Name); err != nil {
		log.Warnf("⚠️  Failed to switch to session: %v\n", err)
		log.Warnf("You can attach manually with: tmux attach -t %s\n", sess.Name)
	}

	return nil
}
