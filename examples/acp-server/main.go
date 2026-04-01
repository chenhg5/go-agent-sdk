// acp-server launches an ACP-compatible agent over stdio.
//
// Any ACP client (Cursor, VS Code, etc.) can connect to this process:
//
//	export ANTHROPIC_AUTH_TOKEN=sk-...
//	export ANTHROPIC_BASE_URL=https://api.anthropic.com  # optional
//	export ANTHROPIC_MODEL=claude-sonnet-4-20250514       # optional
//	go run ./examples/acp-server
//
// The client communicates via newline-delimited JSON-RPC 2.0 on stdin/stdout.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	agentsdk "github.com/chenhg5/go-agent-sdk"
	"github.com/chenhg5/go-agent-sdk/acp"
	"github.com/chenhg5/go-agent-sdk/claude"
)

func main() {
	logger := log.New(os.Stderr, "[acp] ", log.LstdFlags|log.Lmsgprefix)

	srv := acp.NewServer(acp.ServerConfig{
		AgentFactory: createAgent,
		Info: &acp.ImplementationInfo{
			Name:    "go-agent-sdk-acp",
			Title:   "Go Agent SDK (ACP)",
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

func createAgent(_ context.Context, params acp.NewSessionParams) (agentsdk.Agent, error) {
	apiKey := os.Getenv("ANTHROPIC_AUTH_TOKEN")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_AUTH_TOKEN is not set")
	}

	var providerOpts []claude.Option
	if base := os.Getenv("ANTHROPIC_BASE_URL"); base != "" {
		providerOpts = append(providerOpts, claude.WithBaseURL(base))
	}

	model := os.Getenv("ANTHROPIC_MODEL")
	if model == "" {
		model = agentsdk.DefaultModel
	}

	opts := []agentsdk.Option{
		agentsdk.WithProvider(claude.NewProvider(apiKey, providerOpts...)),
		agentsdk.WithModel(model),
		agentsdk.WithMaxTokens(16384),
		agentsdk.WithClaudeCodePreset(),
	}

	if params.CWD != "" {
		opts = append(opts, agentsdk.WithContextProviders(
			agentsdk.StaticContext{Key: "working_directory", Text: "CWD: " + params.CWD},
		))
	}

	return agentsdk.New(opts...)
}
