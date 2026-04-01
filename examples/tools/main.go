// tools demonstrates an agent that uses custom tools to fulfil user requests.
//
// Run:
//
//	export ANTHROPIC_AUTH_TOKEN=sk-...
//	go run ./examples/tools
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	agentsdk "github.com/chenhg5/go-agent-sdk"
	"github.com/chenhg5/go-agent-sdk/claude"
)

// ---------------------------------------------------------------------------
// Weather tool
// ---------------------------------------------------------------------------

type weatherTool struct{}

func (w *weatherTool) Definition() agentsdk.ToolSpec {
	return agentsdk.ToolSpec{
		Name:        "get_weather",
		Description: "Get the current weather for a city.",
		InputSchema: &agentsdk.JSONSchema{
			Type: "object",
			Properties: map[string]*agentsdk.JSONSchema{
				"city": {Type: "string", Description: "City name, e.g. 'Tokyo'"},
			},
			Required: []string{"city"},
		},
	}
}

func (w *weatherTool) Execute(_ context.Context, call agentsdk.ToolCall) (*agentsdk.ToolResult, error) {
	var in struct{ City string }
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return &agentsdk.ToolResult{Content: "invalid input", IsError: true}, nil
	}

	data := map[string]any{
		"city":      in.City,
		"temp_c":    23,
		"condition": "partly cloudy",
		"humidity":  65,
	}
	b, _ := json.Marshal(data)
	fmt.Printf("  [tool] get_weather(%s) → %s\n", in.City, b)
	return &agentsdk.ToolResult{Content: string(b)}, nil
}

// ---------------------------------------------------------------------------
// Time tool
// ---------------------------------------------------------------------------

type timeTool struct{}

func (t *timeTool) Definition() agentsdk.ToolSpec {
	return agentsdk.ToolSpec{
		Name:        "get_time",
		Description: "Get the current date and time in a given timezone (IANA format, e.g. 'Asia/Shanghai').",
		InputSchema: &agentsdk.JSONSchema{
			Type: "object",
			Properties: map[string]*agentsdk.JSONSchema{
				"timezone": {Type: "string", Description: "IANA timezone, e.g. 'America/New_York'"},
			},
			Required: []string{"timezone"},
		},
	}
}

func (t *timeTool) Execute(_ context.Context, call agentsdk.ToolCall) (*agentsdk.ToolResult, error) {
	var in struct{ Timezone string }
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return &agentsdk.ToolResult{Content: "invalid input", IsError: true}, nil
	}

	loc, err := time.LoadLocation(in.Timezone)
	if err != nil {
		return &agentsdk.ToolResult{
			Content: fmt.Sprintf("unknown timezone %q: %v", in.Timezone, err),
			IsError: true,
		}, nil
	}

	now := time.Now().In(loc).Format("2006-01-02 15:04:05 MST")
	fmt.Printf("  [tool] get_time(%s) → %s\n", in.Timezone, now)
	return &agentsdk.ToolResult{Content: now}, nil
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

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
		agentsdk.WithMaxTurns(10),
		agentsdk.WithSystemPrompt("You are a helpful assistant with access to weather and time tools. Use them when appropriate."),
		agentsdk.WithTools(&weatherTool{}, &timeTool{}),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create agent: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	prompt := "What's the weather in Shanghai and what time is it there?"
	fmt.Printf("User: %s\n\n", prompt)

	handler := func(evt agentsdk.Event) {
		switch evt.Type {
		case agentsdk.EventToolUseStart:
			fmt.Printf("  [calling tool: %s]\n", evt.ToolUse.Name)
		case agentsdk.EventTextDelta:
			fmt.Print(evt.Text)
		}
	}

	result, err := agent.RunStream(ctx, prompt, handler)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nrun: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("Reason: %s | Turns: %d | Tokens: in=%d out=%d\n",
		result.Reason,
		len(result.Messages),
		result.Usage.InputTokens,
		result.Usage.OutputTokens,
	)
}
