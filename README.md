# go-agent-sdk

A composable Go SDK for building LLM-powered agents. Inspired by the architecture of Claude Code, designed for embedding into any Go project.

## Features

- **Interface-driven** — every module (Provider, Tool, Executor) is a swappable interface
- **Streaming** — real-time token streaming with event callbacks
- **Tool use** — agentic loop that calls tools and feeds results back automatically
- **Zero external deps** — standard library only (core SDK)
- **Built-in tools** — Bash, FileRead, FileEdit, FileWrite, Glob, Grep out of the box
- **Multi-provider** — Claude built-in; Bedrock, Vertex, OpenAI implementable via `Provider` interface

## Install

```bash
go get github.com/chenhg5/go-agent-sdk
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "os"

    agentsdk "github.com/chenhg5/go-agent-sdk"
    "github.com/chenhg5/go-agent-sdk/claude"
    "github.com/chenhg5/go-agent-sdk/tools"
)

func main() {
    agent, err := agentsdk.New(
        agentsdk.WithProvider(claude.NewProvider(os.Getenv("CLAUDE_API_KEY"))),
        agentsdk.WithSystemPrompt("You are a helpful coding assistant."),
        agentsdk.WithTools(tools.DefaultTools()...),
    )
    if err != nil {
        panic(err)
    }

    result, err := agent.Run(context.Background(), "What is 2+2?")
    if err != nil {
        panic(err)
    }

    last := result.Messages[len(result.Messages)-1]
    fmt.Println(last.TextContent())
}
```

## Streaming

```go
result, err := agent.RunStream(ctx, "Explain Go interfaces.", func(evt agentsdk.Event) {
    switch evt.Type {
    case agentsdk.EventTextDelta:
        fmt.Print(evt.Text)
    case agentsdk.EventToolUseStart:
        fmt.Printf("\n→ calling %s\n", evt.ToolUse.Name)
    case agentsdk.EventToolResult:
        fmt.Printf("← result (%d bytes)\n", len(evt.ToolResultData.Content))
    }
})
```

## Built-in Tools

Register all built-in tools (Bash, FileRead, FileEdit, FileWrite, Glob, Grep) in one call:

```go
import "github.com/chenhg5/go-agent-sdk/tools"

agent, _ := agentsdk.New(
    agentsdk.WithProvider(claude.NewProvider(apiKey)),
    agentsdk.WithTools(tools.DefaultTools()...),
)
```

Or pick individual tools:

```go
agent, _ := agentsdk.New(
    agentsdk.WithProvider(claude.NewProvider(apiKey)),
    agentsdk.WithTools(
        &tools.BashTool{WorkingDir: "/my/project"},
        &tools.FileReadTool{},
        &tools.GrepTool{},
    ),
)
```

## Custom Tools

```go
type TimeTool struct{}

func (t *TimeTool) Definition() agentsdk.ToolSpec {
    return agentsdk.ToolSpec{
        Name:        "current_time",
        Description: "Returns the current UTC time.",
        InputSchema: &agentsdk.JSONSchema{Type: "object"},
    }
}

func (t *TimeTool) Execute(ctx context.Context, call agentsdk.ToolCall) (*agentsdk.ToolResult, error) {
    return &agentsdk.ToolResult{Content: time.Now().UTC().Format(time.RFC3339)}, nil
}

agent, _ := agentsdk.New(
    agentsdk.WithProvider(claude.NewProvider(apiKey)),
    agentsdk.WithTools(&TimeTool{}),
    agentsdk.WithMaxTurns(10),
)
```

## Architecture

```
┌──────────────────────────────────────────────┐
│                 Agent (agent.go)             │  Public API
│  Run / RunStream / RunMessages / Reset       │
├──────────────────────────────────────────────┤
│              Agent Loop (loop.go)            │  Core loop
│  build params → stream LLM → exec tools → ↻  │
├─────────────────────┬────────────────────────┤
│   Provider (i/f)    │   ToolExecutor (i/f)   │  Interfaces
│   └→ claude/        │   └→ Parallel/Seq      │
├─────────────────────┼────────────────────────┤
│   Stream (i/f)      │   Tool (i/f)           │
│   └→ SSE stream     │   └→ custom tools      │
├─────────────────────┴────────────────────────┤
│          Message / ContentBlock / Usage      │  Types
└──────────────────────────────────────────────┘
```

## Configuration Options

| Option | Description | Default |
|---|---|---|
| `WithProvider(p)` | LLM provider (**required**) | — |
| `WithModel(m)` | Model name | `claude-sonnet-4-20250514` |
| `WithSystemPrompt(s)` | System prompt | `""` |
| `WithMaxTokens(n)` | Max output tokens per call | `16384` |
| `WithMaxTurns(n)` | Turn limit (0=unlimited) | `0` |
| `WithTemperature(t)` | Sampling temperature | `nil` |
| `WithTools(t...)` | Register tools | `[]` |
| `WithToolExecutor(e)` | Execution strategy | `ParallelExecutor` |
| `WithThinking(n)` | Extended thinking budget | `nil` |

## Permission Control

The permission handler is called **before every tool execution**. Return `Allow`, `Deny`, or implement interactive approval:

```go
agent, _ := agentsdk.New(
    agentsdk.WithProvider(claude.NewProvider(apiKey)),
    agentsdk.WithTools(tools.DefaultTools()...),
    agentsdk.WithPermissionHandler(func(ctx context.Context, req agentsdk.PermissionRequest) agentsdk.PermissionResponse {
        // Allow read-only tools, deny writes
        if req.Call.Name == "file_read" || req.Call.Name == "glob" || req.Call.Name == "grep" {
            return agentsdk.PermissionResponse{Decision: agentsdk.PermissionAllow}
        }
        fmt.Printf("⚠ Allow %s? [y/n] ", req.Call.Name)
        var answer string
        fmt.Scanln(&answer)
        if answer == "y" {
            return agentsdk.PermissionResponse{Decision: agentsdk.PermissionAllow}
        }
        return agentsdk.PermissionResponse{Decision: agentsdk.PermissionDeny, Reason: "user rejected"}
    }),
)
```

Built-in policies: `agentsdk.AllowAll`, `agentsdk.DenyAll`, `agentsdk.ReadOnlyPermission(registry)`.

## Lifecycle Hooks

```go
agentsdk.WithHooks(&agentsdk.Hooks{
    BeforeToolCall: func(ctx context.Context, call agentsdk.ToolCall) error {
        log.Printf("→ %s", call.Name)
        return nil // return error to block execution
    },
    AfterToolCall: func(ctx context.Context, call agentsdk.ToolCall, result agentsdk.ToolCallResult) {
        log.Printf("← %s (%d bytes, error=%v)", call.Name, len(result.Content), result.IsError)
    },
    AfterTurn: func(ctx context.Context, turn int, usage agentsdk.Usage) {
        log.Printf("turn %d: %d tokens", turn, usage.TotalTokens())
    },
})
```

## Cost Tracking

```go
tracker := agentsdk.NewCostTracker(nil) // nil = use default pricing
agent, _ := agentsdk.New(
    agentsdk.WithProvider(claude.NewProvider(apiKey)),
    agentsdk.WithCostTracker(tracker),
)
result, _ := agent.Run(ctx, "...")
fmt.Printf("Cost: $%.4f (%d tokens)\n", result.Cost, result.Usage.TotalTokens())
```

## Conversation Persistence

```go
store, _ := agentsdk.NewFileStore("./conversations")
agent, _ := agentsdk.New(
    agentsdk.WithProvider(claude.NewProvider(apiKey)),
    agentsdk.WithStore(store, "session-001"),
)
// Automatically loads previous messages on New() and saves after every Run().
```

## Context Window Management

```go
agentsdk.WithCompact(200000,                          // context window size
    agentsdk.CompactThreshold(0.8),                   // trigger at 80%
    agentsdk.CompactWith(&agentsdk.SlidingWindowCompact{KeepFirst: 4, KeepLast: 20}),
)
```

## Multi-turn Conversations

```go
agent.Run(ctx, "What files are in this project?")
agent.Run(ctx, "Now refactor the auth module.")
agent.Reset()
agent.Run(ctx, "Start a new conversation.")
```

## Roadmap

- [x] Phase 1: Core SDK — Agent loop, Provider, Tool, Streaming
- [x] Phase 2: Built-in tools — Bash, FileRead, FileEdit, FileWrite, Glob, Grep
- [x] Phase 3: Advanced — Permission, Hooks, CostTracker, Store, Auto-compact
- [ ] Phase 4: Ecosystem — MCP client, sub-agents, hooks, coordinator mode

## License

MIT
