package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

	// Create a worktree for this session
	worktreePath := filepath.Join(cfg.WorktreeDir, sessionName)
	branchName := "worktree-" + sessionName
	log.Debugf("🌿 Creating worktree at %s (branch: %s)...\n", worktreePath, branchName)
	if err := gitMgr.AddWorktree(baseRepoPath, worktreePath, branchName); err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}
	log.Debugf("✓ Worktree created\n")

	sess := &types.Session{
		Name:         sessionName,
		RepoURL:      selected.URL,
		Title:        sessionName,
		WorktreePath: worktreePath,
		BaseRepoPath: baseRepoPath,
		CreatedAt:    time.Now(),
	}
	return finalizeSession(cfg, sess, sessionMgr, tmuxMgr, log)
}

func createWorkspaceSession(cfg *types.Config, selected *types.Repository, sessionMgr *session.Manager, gitMgr *git.Manager, tmuxMgr *tmux.Manager, log *logging.Logger) error {
	sessionName, err := sessionMgr.GenerateUniqueName(selected.Name)
	if err != nil {
		return fmt.Errorf("failed to generate session name: %w", err)
	}

	workspacePath := filepath.Join(cfg.WorktreeDir, sessionName)
	if err := os.MkdirAll(workspacePath, 0755); err != nil {
		return fmt.Errorf("failed to create workspace directory: %w", err)
	}

	log.Debugf("🌿 Setting up workspace '%s' with %d repos (using worktrees)...\n", selected.Name, len(selected.WorkspaceRepos))

	for _, repoURL := range selected.WorkspaceRepos {
		repoName := git.ExtractRepoName(repoURL)
		dirName := strings.ReplaceAll(repoName, "/", "-")

		log.Debugf("  📦 Ensuring base repo for %s...\n", repoName)
		baseRepoPath, err := gitMgr.EnsureBaseRepo(repoURL, cfg.BaseRepoDir)
		if err != nil {
			return fmt.Errorf("failed to ensure base repo for %s: %w", repoURL, err)
		}

		worktreePath := filepath.Join(workspacePath, dirName)
		branchName := "worktree-" + sessionName + "-" + dirName
		log.Debugf("  🌿 Creating worktree for %s...\n", repoName)
		if err := gitMgr.AddWorktree(baseRepoPath, worktreePath, branchName); err != nil {
			return fmt.Errorf("failed to create worktree for %s: %w", repoURL, err)
		}
		log.Debugf("  ✓ %s ready\n", repoName)
	}

	sess := &types.Session{
		Name:         sessionName,
		RepoURL:      "workspace:" + selected.Name,
		Title:        sessionName,
		RepoURLs:     selected.WorkspaceRepos,
		WorktreePath: workspacePath,
		CreatedAt:    time.Now(),
	}
	return finalizeSession(cfg, sess, sessionMgr, tmuxMgr, log)
}

// finalizeSession creates the tmux session, saves metadata, and switches to it.
func finalizeSession(cfg *types.Config, sess *types.Session, sessionMgr *session.Manager, tmuxMgr *tmux.Manager, log *logging.Logger) error {
	var claudeCmd string
	if cfg.ClaudeBin != "" {
		claudeCmd = cfg.ClaudeBin + " " + strings.Join(cfg.ClaudeArgs, " ")
	}

	log.Debugf("🚀 Creating tmux session '%s'...\n", sess.Name)
	if err := tmuxMgr.CreateSession(sess.Name, sess.WorktreePath, claudeCmd); err != nil {
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
