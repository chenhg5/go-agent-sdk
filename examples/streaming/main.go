// streaming demonstrates real-time event handling with RunStream.
//
// Run:
//
//	export ANTHROPIC_AUTH_TOKEN=sk-...
//	go run ./examples/streaming
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
		agentsdk.WithMaxTokens(2048),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create agent: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	fmt.Println("Streaming response:")
	fmt.Println("---")

	var chunks int
	handler := func(evt agentsdk.Event) {
		switch evt.Type {
		case agentsdk.EventTurnStart:
			fmt.Printf("[turn %d]\n", evt.Turn)
		case agentsdk.EventTextDelta:
			fmt.Print(evt.Text)
			chunks++
		case agentsdk.EventTurnEnd:
			fmt.Println()
		}
	}

	result, err := agent.RunStream(ctx, "Write a short poem about programming in Go.", handler)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nrun: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("---")
	fmt.Printf("Streamed %d chunks | reason=%s | tokens: in=%d out=%d\n",
		chunks, result.Reason, result.Usage.InputTokens, result.Usage.OutputTokens)
}
