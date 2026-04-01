package agentsdk

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// GitContext — mirrors Claude Code's gitStatus context injection
// ---------------------------------------------------------------------------

// GitContext provides git repository information: branch, recent commits,
// and working-tree status. If the directory is not a git repo, it returns
// an appropriate note rather than an error.
type GitContext struct {
	WorkDir string
}

func (g GitContext) Name() string { return "gitStatus" }

func (g GitContext) Provide(ctx context.Context) (string, error) {
	dir := g.WorkDir
	if dir == "" {
		dir, _ = os.Getwd()
	}

	if !isGitRepo(ctx, dir) {
		return "Not a git repository.", nil
	}

	var parts []string

	if branch := gitCmd(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD"); branch != "" {
		parts = append(parts, "Branch: "+branch)
	}

	if status := gitCmd(ctx, dir, "status", "--short"); status != "" {
		lines := strings.Split(status, "\n")
		if len(lines) > 20 {
			lines = append(lines[:20], fmt.Sprintf("... and %d more files", len(lines)-20))
		}
		parts = append(parts, "Changes:\n"+strings.Join(lines, "\n"))
	} else {
		parts = append(parts, "Working tree clean.")
	}

	if log := gitCmd(ctx, dir, "log", "--oneline", "-5"); log != "" {
		parts = append(parts, "Recent commits:\n"+log)
	}

	return strings.Join(parts, "\n"), nil
}

func isGitRepo(ctx context.Context, dir string) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func gitCmd(ctx context.Context, dir string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	result := strings.TrimSpace(string(out))
	if len(result) > 2000 {
		result = result[:2000] + "\n... (truncated)"
	}
	return result
}

// ---------------------------------------------------------------------------
// DateContext — mirrors Claude Code's currentDate context
// ---------------------------------------------------------------------------

// DateContext provides the current date and time.
type DateContext struct{}

func (DateContext) Name() string { return "currentDate" }

func (DateContext) Provide(_ context.Context) (string, error) {
	return "Today's date is " + time.Now().Format("2006-01-02 (Monday)") + ".", nil
}

// ---------------------------------------------------------------------------
// EnvContext — mirrors Claude Code's computeSimpleEnvInfo
// ---------------------------------------------------------------------------

// EnvContext provides environment information: OS, architecture, shell,
// working directory, and model name.
type EnvContext struct {
	WorkDir string
	Model   string
}

func (e EnvContext) Name() string { return "environment" }

func (e EnvContext) Provide(_ context.Context) (string, error) {
	dir := e.WorkDir
	if dir == "" {
		dir, _ = os.Getwd()
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "unknown"
	} else {
		shell = filepath.Base(shell)
	}

	var sb strings.Builder
	sb.WriteString("# Environment\n\n")
	fmt.Fprintf(&sb, "- Primary working directory: %s\n", dir)
	fmt.Fprintf(&sb, "- Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&sb, "- Shell: %s\n", shell)
	if e.Model != "" {
		fmt.Fprintf(&sb, "- Model: %s\n", e.Model)
	}

	return sb.String(), nil
}

// ---------------------------------------------------------------------------
// CLAUDEMDContext — loads project instructions from CLAUDE.md
// ---------------------------------------------------------------------------

// CLAUDEMDContext searches for CLAUDE.md files in standard locations
// and provides their contents as project-level instructions.
//
// Search order (all found files are concatenated):
//  1. {WorkDir}/CLAUDE.md
//  2. {WorkDir}/.claude/CLAUDE.md
//  3. ~/.claude/CLAUDE.md (user-level, if IncludeUser is true)
type CLAUDEMDContext struct {
	WorkDir     string
	IncludeUser bool // also load ~/.claude/CLAUDE.md
}

func (c CLAUDEMDContext) Name() string { return "claudeMd" }

func (c CLAUDEMDContext) Provide(_ context.Context) (string, error) {
	dir := c.WorkDir
	if dir == "" {
		dir, _ = os.Getwd()
	}

	candidates := []string{
		filepath.Join(dir, "CLAUDE.md"),
		filepath.Join(dir, ".claude", "CLAUDE.md"),
	}
	if c.IncludeUser {
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".claude", "CLAUDE.md"))
		}
	}

	var parts []string
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content != "" {
			parts = append(parts, content)
		}
	}

	if len(parts) == 0 {
		return "", nil
	}
	return strings.Join(parts, "\n\n---\n\n"), nil
}

// ---------------------------------------------------------------------------
// StaticContext — injects a fixed string as context
// ---------------------------------------------------------------------------

// StaticContext provides a fixed string. Useful for injecting custom
// project-specific instructions without writing a file.
type StaticContext struct {
	Key  string
	Text string
}

func (s StaticContext) Name() string                         { return s.Key }
func (s StaticContext) Provide(_ context.Context) (string, error) { return s.Text, nil }
