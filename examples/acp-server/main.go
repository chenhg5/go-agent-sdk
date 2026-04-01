// acp-server launches an ACP-compatible coding agent over stdio.
//
// This agent mirrors Claude Code's capabilities:
//   - Claude Code system prompt (identity, rules, tool-usage, tone)
//   - All built-in tools: Bash, FileRead, FileEdit, FileWrite, Glob, Grep
//   - Dynamic context injection: Git status, date, environment, CLAUDE.md
//   - Permission handling via ACP session/request_permission
//
// Usage:
//
//	export ANTHROPIC_AUTH_TOKEN=sk-...
//	export ANTHROPIC_BASE_URL=https://api.anthropic.com  # optional
//	export ANTHROPIC_MODEL=claude-sonnet-4-20250514       # optional
//	go run ./examples/acp-server
//
// Or build a binary for cc-connect:
//
//	go build -o acp-agent ./examples/acp-server
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	agentsdk "github.com/chenhg5/go-agent-sdk"
	"github.com/chenhg5/go-agent-sdk/acp"
	"github.com/chenhg5/go-agent-sdk/claude"
	"github.com/chenhg5/go-agent-sdk/tools"
)

func main() {
	logger := log.New(os.Stderr, "[acp] ", log.LstdFlags|log.Lmsgprefix)

	srv := acp.NewServer(acp.ServerConfig{
		AgentFactory: createAgent,
		Info: &acp.ImplementationInfo{
			Name:    "go-agent-sdk",
			Title:   "Go Agent SDK",
			Version: "0.6.0",
		},
		Capabilities: &acp.AgentCapabilities{
			PromptCapabilities: &acp.PromptCapabilities{
				EmbeddedContext: true,
			},
		},
		Logger: logger,
	})

	logger.Println("starting ACP server on stdio...")
	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "acp server error: %v\n", err)
		os.Exit(1)
	}
}

func createAgent(_ context.Context, params acp.NewSessionParams, perm agentsdk.PermissionHandler) (agentsdk.Agent, error) {
	apiKey := os.Getenv("ANTHROPIC_AUTH_TOKEN")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_AUTH_TOKEN is not set")
	}

	var providerOpts []claude.Option
	baseURL := os.Getenv("ANTHROPIC_BASE_URL")
	if baseURL != "" {
		providerOpts = append(providerOpts, claude.WithBaseURL(baseURL))
	}

	// Third-party proxies often don't support the array-format system parameter.
	if baseURL != "" && !strings.Contains(baseURL, "anthropic.com") {
		providerOpts = append(providerOpts, claude.WithForceStringSystem())
	}

	model := os.Getenv("ANTHROPIC_MODEL")
	if model == "" {
		model = agentsdk.DefaultModel
	}

	cwd := params.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	opts := []agentsdk.Option{
		agentsdk.WithProvider(claude.NewProvider(apiKey, providerOpts...)),
		agentsdk.WithModel(model),
		agentsdk.WithMaxTokens(16384),

		// Claude Code system prompt preset
		agentsdk.WithClaudeCodePreset(),

		// All built-in tools (Bash, FileRead, FileEdit, FileWrite, Glob, Grep)
		agentsdk.WithTools(tools.DefaultTools()...),

		// Dynamic context providers (injected into first user message)
		agentsdk.WithContextProviders(
			agentsdk.DateContext{},
			agentsdk.EnvContext{Model: model, WorkDir: cwd},
			agentsdk.GitContext{WorkDir: cwd},
			agentsdk.CLAUDEMDContext{WorkDir: cwd, IncludeUser: true},
		),
	}

	// ACP permission handler — delegates to the editor via session/request_permission
	if perm != nil {
		opts = append(opts, agentsdk.WithPermissionHandler(perm))
	}

	return agentsdk.New(opts...)
}
