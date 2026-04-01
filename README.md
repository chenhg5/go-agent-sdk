# go-agent-sdk

A composable Go SDK for building LLM-powered agents. Inspired by the architecture of [Claude Code](https://github.com/anthropics/claude-code), designed for embedding into any Go project.

[**中文文档 / Chinese Documentation**](README_CN.md)

## Features

- **Interface-driven** — every module (Provider, Tool, Executor) is a swappable interface
- **Streaming** — real-time token streaming with event callbacks
- **Tool use** — agentic loop that calls tools and feeds results back automatically
- **Zero external deps** — standard library only (core SDK)
- **Built-in tools** — Bash, FileRead, FileEdit, FileWrite, Glob, Grep out of the box
- **Multi-provider** — Claude built-in; OpenAI, Bedrock, Vertex implementable via `Provider` interface
- **Permission control** — sync/async interactive approval; agent loop pauses until user decides
- **MCP support** — dynamically discover and call external tools via Model Context Protocol
- **Sub-agents** — delegate tasks to child agents and collect results
- **ACP protocol** — expose the agent as a standard [Agent Client Protocol](https://agentclientprotocol.com) server (stdio JSON-RPC)

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
)

func main() {
    agent, err := agentsdk.New(
        agentsdk.WithProvider(claude.NewProvider(os.Getenv("ANTHROPIC_AUTH_TOKEN"))),
        agentsdk.WithSystemPrompt("You are a helpful coding assistant."),
    )
    if err != nil {
        panic(err)
    }

    result, err := agent.Run(context.Background(), "What is a goroutine in Go?")
    if err != nil {
        panic(err)
    }

    fmt.Println(result.Messages[len(result.Messages)-1].TextContent())
}
```

## Streaming

```go
result, err := agent.RunStream(ctx, "Explain Go interfaces.", func(evt agentsdk.Event) {
    switch evt.Type {
    case agentsdk.EventTextDelta:
        fmt.Print(evt.Text)
    case agentsdk.EventToolUseStart:
        fmt.Printf("\n-> calling %s\n", evt.ToolUse.Name)
    case agentsdk.EventToolResult:
        fmt.Printf("<- result (%d bytes)\n", len(evt.ToolResultData.Content))
    }
})
```

## Built-in Tools

Register all built-in tools in one call:

```go
import "github.com/chenhg5/go-agent-sdk/tools"

agent, _ := agentsdk.New(
    agentsdk.WithProvider(claude.NewProvider(apiKey)),
    agentsdk.WithTools(tools.DefaultTools()...),
)
```

| Tool | Description |
|---|---|
| `bash` | Shell command execution (timeout, output truncation) |
| `file_read` | File reading (line numbers, offset, binary detection) |
| `file_edit` | File editing (find & replace, unique match validation) |
| `file_write` | File writing/creation (auto-creates parent directories) |
| `glob` | Recursive file matching (`**` patterns) |
| `grep` | Regex content search (recursive, glob filtering) |

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
```

## Permission Control

The permission handler is called **before every tool execution**. The handler can block (e.g. waiting for user input), and the agent loop pauses naturally until it returns:

```go
agentsdk.WithPermissionHandler(func(ctx context.Context, req agentsdk.PermissionRequest) agentsdk.PermissionResponse {
    if req.Call.Name == "file_read" || req.Call.Name == "grep" {
        return agentsdk.PermissionResponse{Decision: agentsdk.PermissionAllow}
    }
    fmt.Printf("Allow %s? [y/n] ", req.Call.Name)
    var answer string
    fmt.Scanln(&answer)
    if answer == "y" {
        return agentsdk.PermissionResponse{Decision: agentsdk.PermissionAllow}
    }
    return agentsdk.PermissionResponse{Decision: agentsdk.PermissionDeny, Reason: "user rejected"}
})
```

Built-in policies: `AllowAll`, `DenyAll`, `ReadOnlyPermission(registry)`.

### Event-driven permissions (Web / TUI)

For UIs where permission decisions arrive asynchronously:

```go
requests := make(chan agentsdk.PermissionRequest, 1)
responses := make(chan agentsdk.PermissionResponse, 1)

agent, _ := agentsdk.New(
    agentsdk.WithProvider(provider),
    agentsdk.WithPermissionHandler(agentsdk.ChannelPermission(requests, responses)),
)

go func() {
    for req := range requests {
        // show confirmation dialog ...
        responses <- agentsdk.PermissionResponse{Decision: agentsdk.PermissionAllow}
    }
}()
```

### Two-phase permissions (tool policy + interactive confirmation)

```go
agentsdk.WithPermissionHandler(agentsdk.WithToolCheckerAndPrompter(registry, prompter))
```

The handler can also modify tool input before execution via `ModifiedInput`:

```go
return agentsdk.PermissionResponse{
    Decision:      agentsdk.PermissionAllow,
    ModifiedInput: sanitisedJSON,
}
```

## MCP Tool Integration

Discover and call tools from any [MCP](https://modelcontextprotocol.io/) server:

```go
import "github.com/chenhg5/go-agent-sdk/mcp"

client, _ := mcp.NewStdioClient(ctx, "npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp")
defer client.Close()

mcpTools, _ := mcp.ToolsFromClient(client)

agent, _ := agentsdk.New(
    agentsdk.WithProvider(provider),
    agentsdk.WithTools(mcpTools...),
)
```

## Sub-Agents

Expose another Agent as a tool for task delegation:

```go
researcher, _ := agentsdk.New(
    agentsdk.WithProvider(provider),
    agentsdk.WithSystemPrompt("You are a research assistant."),
    agentsdk.WithTools(tools.DefaultTools()...),
)

mainAgent, _ := agentsdk.New(
    agentsdk.WithProvider(provider),
    agentsdk.WithTools(&agentsdk.SubAgentTool{
        AgentName:   "researcher",
        Description: "Delegate research tasks to a specialist.",
        SubAgent:    researcher,
    }),
)
```

## Lifecycle Hooks

```go
agentsdk.WithHooks(&agentsdk.Hooks{
    BeforeToolCall: func(ctx context.Context, call agentsdk.ToolCall) error {
        log.Printf("-> %s", call.Name)
        return nil // return error to block execution
    },
    AfterToolCall: func(ctx context.Context, call agentsdk.ToolCall, result agentsdk.ToolCallResult) {
        log.Printf("<- %s (%d bytes, error=%v)", call.Name, len(result.Content), result.IsError)
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
    agentsdk.WithProvider(provider),
    agentsdk.WithCostTracker(tracker),
)
result, _ := agent.Run(ctx, "...")
fmt.Printf("Cost: $%.4f (%d tokens)\n", result.Cost, result.Usage.TotalTokens())
```

## Conversation Persistence

```go
store, _ := agentsdk.NewFileStore("./conversations")
agent, _ := agentsdk.New(
    agentsdk.WithProvider(provider),
    agentsdk.WithStore(store, "session-001"),
)
// Automatically loads previous messages on New() and saves after every Run().
```

## Context Window Management

```go
agentsdk.WithCompact(200000,
    agentsdk.CompactThreshold(0.8),
    agentsdk.CompactWith(&agentsdk.SlidingWindowCompact{KeepFirst: 4, KeepLast: 20}),
)
```

## System Prompt Engineering

The SDK provides a structured prompt assembly system aligned with Claude Code's multi-section architecture — including cache boundaries, dynamic context injection, and preset templates.

### Method 1: Simple string (backward compatible)

```go
agentsdk.WithSystemPrompt("You are a helpful coding assistant.")
```

### Method 2: Claude Code Preset

Pre-built sections mirroring Claude Code's system prompt (identity, system rules, task guidelines, tool usage, tone, output efficiency):

```go
agentsdk.WithClaudeCodePreset()

// Or with appended instructions:
agentsdk.WithClaudeCodePreset("Always respond in Chinese.")
```

### Method 3: PromptBuilder (full control)

Assemble multi-section prompts with cache boundaries for Anthropic's prompt caching:

```go
builder := agentsdk.NewPromptBuilder().
    CachedSection("identity", "You are an expert Go developer.", 10).
    CachedSection("rules", "# Rules\nAlways use error wrapping.", 20).
    Section("env", envInfo, 30). // dynamic, not cached
    Append("Focus on performance.")

agent, _ := agentsdk.New(
    agentsdk.WithProvider(provider),
    agentsdk.WithPromptBuilder(builder),
)
```

`BuildBlocks()` produces structured system blocks with `cache_control` markers — the last cached block gets `{"type": "ephemeral"}`, enabling prompt caching across turns.

### Method 4: Append to any prompt

```go
agentsdk.WithSystemPrompt("You are a code reviewer."),
agentsdk.WithAppendPrompt("Rate code quality 1-10 for every review.")
```

### Dynamic Context Injection

ContextProviders inject environment information into the first user message, wrapped in `<system-reminder>` tags (matching Claude Code's pattern):

```go
agent, _ := agentsdk.New(
    agentsdk.WithProvider(provider),
    agentsdk.WithClaudeCodePreset(),
    agentsdk.WithContextProviders(
        agentsdk.GitContext{WorkDir: "."},         // branch, status, recent commits
        agentsdk.DateContext{},                     // current date
        agentsdk.EnvContext{Model: "claude-sonnet"}, // OS, shell, working dir
        agentsdk.CLAUDEMDContext{WorkDir: ".", IncludeUser: true}, // CLAUDE.md project instructions
    ),
)
```

Built-in providers:

| Provider | Content |
|---|---|
| `GitContext` | Branch, file changes, recent 5 commits |
| `DateContext` | Current date |
| `EnvContext` | OS, architecture, shell, working directory, model |
| `CLAUDEMDContext` | Project instructions from `CLAUDE.md` / `.claude/CLAUDE.md` |
| `StaticContext` | Fixed custom text |
| `ContextProviderFunc` | Function adapter for one-off providers |

## ACP Protocol Server

Expose your agent as an [Agent Client Protocol](https://agentclientprotocol.com) server, enabling any ACP-compatible editor (Cursor, VS Code, etc.) to connect:

```go
import "github.com/chenhg5/go-agent-sdk/acp"

srv := acp.NewServer(acp.ServerConfig{
    AgentFactory: func(ctx context.Context, params acp.NewSessionParams) (agentsdk.Agent, error) {
        return agentsdk.New(
            agentsdk.WithProvider(provider),
            agentsdk.WithClaudeCodePreset(),
            agentsdk.WithTools(tools.DefaultTools()...),
        )
    },
    Info: &acp.ImplementationInfo{
        Name: "my-agent", Title: "My Agent", Version: "1.0.0",
    },
})

// Blocks on stdin/stdout — the editor launches this as a subprocess.
srv.Run()
```

The server handles the full ACP lifecycle:
- `initialize` — version & capability negotiation
- `session/new` — creates a new Agent session
- `session/prompt` — sends user messages, streams `session/update` notifications back
- `session/cancel` — cancels ongoing prompt turns
- `session/request_permission` — reverse-calls the client for tool permission approval

```bash
go run ./examples/acp-server
```

## Multi-turn Conversations

```go
agent.Run(ctx, "What files are in this project?")
agent.Run(ctx, "Now refactor the auth module.")
agent.Reset()
agent.Run(ctx, "Start a new conversation.")
```

## Configuration Options

| Option | Description | Default |
|---|---|---|
| `WithProvider(p)` | LLM provider (**required**) | -- |
| `WithModel(m)` | Model name | `claude-sonnet-4-20250514` |
| `WithSystemPrompt(s)` | System prompt (plain string) | `""` |
| `WithClaudeCodePreset(append?)` | Claude Code-aligned system prompt | -- |
| `WithPromptBuilder(b)` | Structured multi-section prompt | `nil` |
| `WithAppendPrompt(s)` | Append text after system prompt | `""` |
| `WithContextProviders(p...)` | Dynamic context injection | `[]` |
| `WithMaxTokens(n)` | Max output tokens per call | `16384` |
| `WithMaxTurns(n)` | Turn limit (0 = unlimited) | `0` |
| `WithTemperature(t)` | Sampling temperature | `nil` |
| `WithTools(t...)` | Register tools | `[]` |
| `WithToolExecutor(e)` | Execution strategy | `ParallelExecutor` |
| `WithThinking(n)` | Extended thinking token budget | `nil` |
| `WithPermissionHandler(h)` | Permission callback | `nil` (allow all) |
| `WithHooks(h)` | Lifecycle hooks | `nil` |
| `WithCostTracker(ct)` | Cost tracker | `nil` |
| `WithStore(s, id)` | Conversation persistence | `nil` |
| `WithCompact(n, opts...)` | Context window compaction | `nil` |

## Architecture

```
+----------------------------------------------+
|                 Agent (agent.go)             |  Public API
|  Run / RunStream / RunMessages / Reset       |
+----------------------------------------------+
|        PromptBuilder + ContextProviders      |  Prompt Assembly
|  sections → cache boundary → dynamic ctx     |  (Phase 5)
+----------------------------------------------+
|              Agent Loop (loop.go)            |  Core loop
|  resolve prompt → stream LLM → exec tools   |
+---------------------+------------------------+
|   Provider (i/f)    |   ToolExecutor (i/f)   |  Swappable
|    -> claude/        |    -> Parallel/Seq      |  interfaces
+---------------------+------------------------+
|   Stream (i/f)      |   Tool (i/f)           |
|    -> SSE stream     |    -> built-in / custom |
+---------------------+------------------------+
|   MCP Client        |   SubAgentTool         |  Ecosystem
|    -> tool discovery  |    -> task delegation   |
+---------------------+------------------------+
|   ACP Server (acp/)                          |  Protocol
|    -> stdio JSON-RPC -> Editor integration    |
+----------------------------------------------+
|   Permission | Hooks | CostTracker | Store   |  Advanced
+----------------------------------------------+
```

## Examples

See the [`examples/`](examples/) directory:

- **[basic](examples/basic/)** — simple prompt and response
- **[tools](examples/tools/)** — weather + time tools with streaming
- **[streaming](examples/streaming/)** — real-time event handling
- **[acp-server](examples/acp-server/)** — ACP protocol server over stdio

```bash
export ANTHROPIC_AUTH_TOKEN=sk-...
go run ./examples/basic
go run ./examples/tools
go run ./examples/streaming
```

## Development

```bash
make build            # compile all packages
make test             # unit tests
make test-v           # verbose test output
make test-integration # integration tests (requires ANTHROPIC_AUTH_TOKEN)
make fmt              # format code
make vet              # static analysis
```

## Roadmap

- [x] Phase 1: Core SDK — Agent loop, Provider, Tool, Streaming
- [x] Phase 2: Built-in tools — Bash, FileRead, FileEdit, FileWrite, Glob, Grep
- [x] Phase 3: Advanced — Permission, Hooks, CostTracker, Store, Auto-compact
- [x] Phase 4: Ecosystem — MCP client, sub-agents, interactive permissions
- [x] Phase 5: Prompt Engineering — PromptBuilder, cache boundaries, presets, ContextProviders
- [x] Phase 6: ACP Protocol — Agent Client Protocol server (stdio JSON-RPC, session management, streaming)
- [ ] More providers: OpenAI, Bedrock, Vertex
- [ ] Coordinator mode: multi-agent orchestration

## License

MIT
