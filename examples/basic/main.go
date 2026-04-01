// basic demonstrates the simplest usage: send a prompt and print the reply.
//
// Run:
//
//	export ANTHROPIC_AUTH_TOKEN=sk-...
//	export ANTHROPIC_BASE_URL=https://api.anthropic.com   # optional
//	export ANTHROPIC_MODEL=claude-sonnet-4-20250514        # optional
//	go run ./examples/basic
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	agentsdk "github.com/chenhg5/go-agent-sdk"
	"github.com/chenhg5/go-agent-sdk/claude"
)

func main() {
	apiKey := os.Getenv("ANTHROPIC_AUTH_TOKEN")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "set ANTHROPIC_AUTH_TOKEN to run this example")
		os.Exit(1)
	}

	var providerOpts []claude.Option
	if base := os.Getenv("ANTHROPIC_BASE_URL"); base != "" {
		providerOpts = append(providerOpts, claude.WithBaseURL(base))
	}

	model := os.Getenv("ANTHROPIC_MODEL")
	if model == "" {
		model = agentsdk.DefaultModel
	}

	agent, err := agentsdk.New(
		agentsdk.WithProvider(claude.NewProvider(apiKey, providerOpts...)),
		agentsdk.WithModel(model),
		agentsdk.WithMaxTokens(1024),
		agentsdk.WithSystemPrompt("You are a helpful assistant. Reply concisely."),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create agent: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := agent.Run(ctx, "Explain what a goroutine is in Go, in two sentences.")
	if err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		os.Exit(1)
	}

	reply := result.Messages[len(result.Messages)-1].TextContent()
	fmt.Println(reply)
	fmt.Printf("\n--- usage: input=%d output=%d ---\n", result.Usage.InputTokens, result.Usage.OutputTokens)
}
