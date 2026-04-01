//go:build integration

package agentsdk_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	agentsdk "github.com/chenhg5/go-agent-sdk"
	"github.com/chenhg5/go-agent-sdk/claude"
)

func envProvider(t *testing.T) (*claude.Provider, string) {
	t.Helper()
	apiKey := os.Getenv("ANTHROPIC_AUTH_TOKEN")
	baseURL := os.Getenv("ANTHROPIC_BASE_URL")
	model := os.Getenv("ANTHROPIC_MODEL")
	if apiKey == "" {
		t.Skip("ANTHROPIC_AUTH_TOKEN not set; skipping integration test")
	}
	var opts []claude.Option
	if baseURL != "" {
		opts = append(opts, claude.WithBaseURL(baseURL))
	}
	return claude.NewProvider(apiKey, opts...), model
}

// ---------------------------------------------------------------------------
// Simple chat
// ---------------------------------------------------------------------------

func TestIntegration_SimpleChat(t *testing.T) {
	provider, model := envProvider(t)

	a, err := agentsdk.New(
		agentsdk.WithProvider(provider),
		agentsdk.WithModel(model),
		agentsdk.WithMaxTokens(256),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := a.Run(ctx, "Please reply with exactly: PONG")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if result.Reason != agentsdk.ReasonEndTurn {
		t.Fatalf("reason = %q", result.Reason)
	}

	last := result.Messages[len(result.Messages)-1]
	if !strings.Contains(strings.ToUpper(last.TextContent()), "PONG") {
		t.Errorf("expected PONG in response, got: %q", last.TextContent())
	}
	t.Logf("response: %s", last.TextContent())
	t.Logf("usage: in=%d out=%d", result.Usage.InputTokens, result.Usage.OutputTokens)
}

// ---------------------------------------------------------------------------
// Streaming
// ---------------------------------------------------------------------------

func TestIntegration_Streaming(t *testing.T) {
	provider, model := envProvider(t)

	a, err := agentsdk.New(
		agentsdk.WithProvider(provider),
		agentsdk.WithModel(model),
		agentsdk.WithMaxTokens(128),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var textChunks []string
	handler := func(evt agentsdk.Event) {
		if evt.Type == agentsdk.EventTextDelta {
			textChunks = append(textChunks, evt.Text)
		}
	}

	result, err := a.RunStream(ctx, "Say hello in one sentence.", handler)
	if err != nil {
		t.Fatal(err)
	}

	if len(textChunks) == 0 {
		t.Error("expected streaming text chunks, got none")
	}

	combined := strings.Join(textChunks, "")
	last := result.Messages[len(result.Messages)-1]
	if combined != last.TextContent() {
		t.Errorf("streamed text doesn't match final message:\n  stream: %q\n  final:  %q", combined, last.TextContent())
	}
	t.Logf("streamed %d chunks, total: %q", len(textChunks), combined)
}

// ---------------------------------------------------------------------------
// Tool use
// ---------------------------------------------------------------------------

type weatherTool struct{}

type weatherInput struct {
	City string `json:"city"`
}

func (w *weatherTool) Definition() agentsdk.ToolSpec {
	return agentsdk.ToolSpec{
		Name:        "get_weather",
		Description: "Get the current weather for a city. Returns temperature and conditions.",
		InputSchema: &agentsdk.JSONSchema{
			Type: "object",
			Properties: map[string]*agentsdk.JSONSchema{
				"city": {Type: "string", Description: "City name"},
			},
			Required: []string{"city"},
		},
	}
}

func (w *weatherTool) Execute(_ context.Context, call agentsdk.ToolCall) (*agentsdk.ToolResult, error) {
	var in weatherInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return &agentsdk.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}
	result := fmt.Sprintf(`{"city": %q, "temp_c": 22, "condition": "sunny"}`, in.City)
	return &agentsdk.ToolResult{Content: result}, nil
}

func TestIntegration_ToolUse(t *testing.T) {
	provider, model := envProvider(t)

	a, err := agentsdk.New(
		agentsdk.WithProvider(provider),
		agentsdk.WithModel(model),
		agentsdk.WithMaxTokens(512),
		agentsdk.WithMaxTurns(5),
		agentsdk.WithTools(&weatherTool{}),
		agentsdk.WithSystemPrompt("You are a helpful assistant. When asked about weather, always use the get_weather tool."),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var toolUsed bool
	handler := func(evt agentsdk.Event) {
		if evt.Type == agentsdk.EventToolUseStart {
			t.Logf("tool called: %s (id=%s)", evt.ToolUse.Name, evt.ToolUse.ID)
			toolUsed = true
		}
		if evt.Type == agentsdk.EventTextDelta {
			fmt.Print(evt.Text)
		}
	}

	result, err := a.RunStream(ctx, "What's the weather like in Beijing?", handler)
	fmt.Println()
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if !toolUsed {
		t.Error("expected get_weather tool to be called")
	}

	last := result.Messages[len(result.Messages)-1]
	text := strings.ToLower(last.TextContent())
	if !strings.Contains(text, "22") && !strings.Contains(text, "sunny") && !strings.Contains(text, "beijing") {
		t.Errorf("response doesn't mention weather data: %q", last.TextContent())
	}
	t.Logf("final response: %s", last.TextContent())
	t.Logf("usage: in=%d out=%d, turns=%d", result.Usage.InputTokens, result.Usage.OutputTokens, len(result.Messages))
}
