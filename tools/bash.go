package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	agentsdk "github.com/chenhg5/go-agent-sdk"
)

const (
	bashDefaultTimeout = 120 * time.Second
	bashMaxOutput      = 512 * 1024 // 512 KiB
)

// BashTool executes shell commands and returns their output.
type BashTool struct {
	// WorkingDir overrides the working directory for every command.
	// Empty means the process's current directory.
	WorkingDir string
	// Shell overrides the shell binary (default: "bash" on unix, "cmd" on windows).
	Shell string
}

type bashInput struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"` // seconds; 0 = default
}

func (t *BashTool) Definition() agentsdk.ToolSpec {
	return agentsdk.ToolSpec{
		Name:        "bash",
		Description: "Execute a shell command and return its stdout/stderr. Use for running programs, installing packages, or inspecting the system.",
		InputSchema: &agentsdk.JSONSchema{
			Type: "object",
			Properties: map[string]*agentsdk.JSONSchema{
				"command": {
					Type:        "string",
					Description: "The shell command to execute.",
				},
				"timeout": {
					Type:        "integer",
					Description: "Maximum execution time in seconds. Default 120.",
				},
			},
			Required: []string{"command"},
		},
	}
}

func (t *BashTool) Execute(ctx context.Context, call agentsdk.ToolCall) (*agentsdk.ToolResult, error) {
	var in bashInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return &agentsdk.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}
	if strings.TrimSpace(in.Command) == "" {
		return &agentsdk.ToolResult{Content: "command must not be empty", IsError: true}, nil
	}

	timeout := bashDefaultTimeout
	if in.Timeout > 0 {
		timeout = time.Duration(in.Timeout) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	shell, flag := t.shellBin()
	cmd := exec.CommandContext(ctx, shell, flag, in.Command)
	if t.WorkingDir != "" {
		cmd.Dir = t.WorkingDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	out := buildBashOutput(&stdout, &stderr, err, ctx.Err())
	return &agentsdk.ToolResult{Content: out, IsError: err != nil}, nil
}

func (t *BashTool) shellBin() (string, string) {
	if t.Shell != "" {
		return t.Shell, "-c"
	}
	if runtime.GOOS == "windows" {
		return "cmd", "/C"
	}
	return "bash", "-c"
}

func buildBashOutput(stdout, stderr *bytes.Buffer, runErr, ctxErr error) string {
	var sb strings.Builder
	if stdout.Len() > 0 {
		s := truncate(stdout.String(), bashMaxOutput)
		sb.WriteString(s)
	}
	if stderr.Len() > 0 {
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString("[stderr]\n")
		sb.WriteString(truncate(stderr.String(), bashMaxOutput))
	}
	if ctxErr == context.DeadlineExceeded {
		sb.WriteString("\n[error] command timed out")
	} else if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			fmt.Fprintf(&sb, "\n[exit code: %d]", exitErr.ExitCode())
		} else {
			fmt.Fprintf(&sb, "\n[error] %v", runErr)
		}
	}
	if sb.Len() == 0 {
		return "(no output)"
	}
	return sb.String()
}

func truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "\n... (output truncated)"
}
