# go-agent-sdk

可组合的 Go SDK，用于构建 LLM 驱动的 Agent。参考 [Claude Code](https://github.com/anthropics/claude-code) 架构设计，适合嵌入任何 Go 项目。

[**English Documentation**](README.md)

## 特性

- **接口驱动** — Provider、Tool、Executor 均为可替换接口
- **流式响应** — 实时 token 推送，事件回调
- **工具调用** — 自动化 Agent 循环：LLM 调用 → 工具执行 → 结果回传
- **零外部依赖** — 核心 SDK 仅使用标准库
- **内置工具** — Bash、文件读写、Glob、Grep 开箱即用
- **多 Provider** — 内置 Claude，可通过 `Provider` 接口对接 OpenAI、Bedrock 等
- **权限控制** — 支持同步/异步交互式权限确认，Agent 循环自动暂停等待用户决策
- **MCP 支持** — 通过 Model Context Protocol 动态发现和调用外部工具
- **子 Agent** — 支持任务委派给子 Agent 并收集结果
- **Prompt 工程** — 结构化 Prompt 组装、缓存分界线、预设模板、动态上下文注入，对齐 Claude Code 架构

## 安装

```bash
go get github.com/chenhg5/go-agent-sdk
```

## 快速开始

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
        agentsdk.WithSystemPrompt("你是一个有用的助手。"),
    )
    if err != nil {
        panic(err)
    }

    result, err := agent.Run(context.Background(), "Go 的 goroutine 是什么？")
    if err != nil {
        panic(err)
    }

    fmt.Println(result.Messages[len(result.Messages)-1].TextContent())
}
```

## 流式输出

```go
result, err := agent.RunStream(ctx, "解释 Go 的接口机制", func(evt agentsdk.Event) {
    switch evt.Type {
    case agentsdk.EventTextDelta:
        fmt.Print(evt.Text) // 实时打印
    case agentsdk.EventToolUseStart:
        fmt.Printf("\n→ 调用工具: %s\n", evt.ToolUse.Name)
    case agentsdk.EventToolResult:
        fmt.Printf("← 结果 (%d 字节)\n", len(evt.ToolResultData.Content))
    }
})
```

## 内置工具

一行注册所有内置工具：

```go
import "github.com/chenhg5/go-agent-sdk/tools"

agent, _ := agentsdk.New(
    agentsdk.WithProvider(claude.NewProvider(apiKey)),
    agentsdk.WithTools(tools.DefaultTools()...),
)
```

| 工具 | 说明 |
|---|---|
| `bash` | Shell 命令执行（超时控制、输出截断） |
| `file_read` | 文件读取（行号、偏移、二进制检测） |
| `file_edit` | 文件编辑（查找替换、唯一匹配校验） |
| `file_write` | 文件写入/创建（自动创建目录） |
| `glob` | 递归文件匹配（`**` 模式） |
| `grep` | 正则内容搜索（目录递归、glob 过滤） |

## 自定义工具

```go
type WeatherTool struct{}

func (t *WeatherTool) Definition() agentsdk.ToolSpec {
    return agentsdk.ToolSpec{
        Name:        "get_weather",
        Description: "获取指定城市的天气",
        InputSchema: &agentsdk.JSONSchema{
            Type: "object",
            Properties: map[string]*agentsdk.JSONSchema{
                "city": {Type: "string", Description: "城市名称"},
            },
            Required: []string{"city"},
        },
    }
}

func (t *WeatherTool) Execute(ctx context.Context, call agentsdk.ToolCall) (*agentsdk.ToolResult, error) {
    var in struct{ City string }
    json.Unmarshal(call.Input, &in)
    return &agentsdk.ToolResult{Content: fmt.Sprintf(`{"city":"%s","temp":22}`, in.City)}, nil
}
```

## 权限控制

权限 handler 在**每次工具执行前**被调用。handler 函数可以阻塞等待用户确认，Agent 循环会自然暂停：

```go
agentsdk.WithPermissionHandler(func(ctx context.Context, req agentsdk.PermissionRequest) agentsdk.PermissionResponse {
    // 读操作自动放行
    if req.Call.Name == "file_read" || req.Call.Name == "grep" {
        return agentsdk.PermissionResponse{Decision: agentsdk.PermissionAllow}
    }
    // 写操作交互确认（阻塞等待用户输入）
    fmt.Printf("⚠ 允许执行 %s? [y/n] ", req.Call.Name)
    var answer string
    fmt.Scanln(&answer)
    if answer == "y" {
        return agentsdk.PermissionResponse{Decision: agentsdk.PermissionAllow}
    }
    return agentsdk.PermissionResponse{Decision: agentsdk.PermissionDeny, Reason: "用户拒绝"}
})
```

### 事件驱动权限（Web/TUI 场景）

```go
requests := make(chan agentsdk.PermissionRequest, 1)
responses := make(chan agentsdk.PermissionResponse, 1)

agent, _ := agentsdk.New(
    agentsdk.WithProvider(provider),
    agentsdk.WithPermissionHandler(agentsdk.ChannelPermission(requests, responses)),
)

// 在 UI 协程中处理:
go func() {
    for req := range requests {
        // 展示确认对话框...
        responses <- agentsdk.PermissionResponse{Decision: agentsdk.PermissionAllow}
    }
}()
```

### 两阶段权限（工具策略 + 交互确认）

```go
agentsdk.WithPermissionHandler(agentsdk.WithToolCheckerAndPrompter(registry, prompter))
```

内置策略：`AllowAll`、`DenyAll`、`ReadOnlyPermission(registry)`

## MCP 工具集成

通过 Model Context Protocol 连接外部工具服务器：

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

## 子 Agent

将另一个 Agent 作为工具暴露，实现任务委派：

```go
researcher, _ := agentsdk.New(
    agentsdk.WithProvider(provider),
    agentsdk.WithSystemPrompt("你是一个研究助手，擅长查找信息。"),
    agentsdk.WithTools(tools.DefaultTools()...),
)

mainAgent, _ := agentsdk.New(
    agentsdk.WithProvider(provider),
    agentsdk.WithTools(&agentsdk.SubAgentTool{
        AgentName:   "researcher",
        Description: "委派研究任务给研究助手",
        SubAgent:    researcher,
    }),
)
```

## 生命周期钩子

```go
agentsdk.WithHooks(&agentsdk.Hooks{
    BeforeToolCall: func(ctx context.Context, call agentsdk.ToolCall) error {
        log.Printf("→ %s", call.Name)
        return nil // 返回 error 可阻止执行
    },
    AfterTurn: func(ctx context.Context, turn int, usage agentsdk.Usage) {
        log.Printf("轮次 %d: %d tokens", turn, usage.TotalTokens())
    },
})
```

## 费用追踪

```go
tracker := agentsdk.NewCostTracker(nil) // nil = 使用默认定价
agent, _ := agentsdk.New(
    agentsdk.WithProvider(provider),
    agentsdk.WithCostTracker(tracker),
)
result, _ := agent.Run(ctx, "...")
fmt.Printf("费用: $%.4f (%d tokens)\n", result.Cost, result.Usage.TotalTokens())
```

## 会话持久化

```go
store, _ := agentsdk.NewFileStore("./conversations")
agent, _ := agentsdk.New(
    agentsdk.WithProvider(provider),
    agentsdk.WithStore(store, "session-001"),
)
// New() 时自动加载历史消息，Run() 后自动保存
```

## 上下文窗口管理

```go
agentsdk.WithCompact(200000,
    agentsdk.CompactThreshold(0.8),
    agentsdk.CompactWith(&agentsdk.SlidingWindowCompact{KeepFirst: 4, KeepLast: 20}),
)
```

## System Prompt 工程

SDK 提供了对齐 Claude Code 多段落架构的结构化 Prompt 组装系统，支持缓存分界线、动态上下文注入和预设模板。

### 方式 1：简单字符串（向后兼容）

```go
agentsdk.WithSystemPrompt("你是一个有用的编码助手。")
```

### 方式 2：Claude Code 预设

内置 Claude Code 的系统提示词段落（身份、规则、任务指南、工具用法、语气、输出效率）：

```go
agentsdk.WithClaudeCodePreset()

// 追加自定义指令:
agentsdk.WithClaudeCodePreset("始终用中文回答。")
```

### 方式 3：PromptBuilder（完全控制）

多段落组装 + Anthropic Prompt 缓存支持：

```go
builder := agentsdk.NewPromptBuilder().
    CachedSection("identity", "你是 Go 专家。", 10).          // 可缓存段
    CachedSection("rules", "# 规则\n始终使用 error wrapping。", 20).
    Section("env", envInfo, 30).                               // 动态段
    Append("注重性能优化。")

agent, _ := agentsdk.New(
    agentsdk.WithProvider(provider),
    agentsdk.WithPromptBuilder(builder),
)
```

`BuildBlocks()` 生成带 `cache_control` 标记的结构化 System Block，最后一个 cached 段带 `{"type": "ephemeral"}`，实现跨轮次的 Prompt 缓存。

### 方式 4：追加模式

```go
agentsdk.WithSystemPrompt("你是代码审查助手。"),
agentsdk.WithAppendPrompt("每次审查都给代码质量打 1-10 分。")
```

### 动态上下文注入

ContextProvider 将环境信息注入首条用户消息，包裹在 `<system-reminder>` 标签中（与 Claude Code 一致）：

```go
agent, _ := agentsdk.New(
    agentsdk.WithProvider(provider),
    agentsdk.WithClaudeCodePreset(),
    agentsdk.WithContextProviders(
        agentsdk.GitContext{WorkDir: "."},           // 分支、变更、最近提交
        agentsdk.DateContext{},                       // 当前日期
        agentsdk.EnvContext{Model: "claude-sonnet"},  // 操作系统、Shell、工作目录
        agentsdk.CLAUDEMDContext{WorkDir: ".", IncludeUser: true}, // CLAUDE.md 项目指令
    ),
)
```

内置 Provider：

| Provider | 提供内容 |
|---|---|
| `GitContext` | 分支名、文件变更、最近 5 次提交 |
| `DateContext` | 当前日期 |
| `EnvContext` | 操作系统、架构、Shell、工作目录、模型名 |
| `CLAUDEMDContext` | 项目指令（`CLAUDE.md` / `.claude/CLAUDE.md`） |
| `StaticContext` | 自定义固定文本 |
| `ContextProviderFunc` | 函数适配器（一次性 Provider） |

## 多轮对话

```go
agent.Run(ctx, "这个项目有哪些文件？")
agent.Run(ctx, "重构一下 auth 模块")
agent.Reset() // 清空历史
agent.Run(ctx, "开始新对话")
```

## 配置选项一览

| 选项 | 说明 | 默认值 |
|---|---|---|
| `WithProvider(p)` | LLM Provider（**必填**） | — |
| `WithModel(m)` | 模型名称 | `claude-sonnet-4-20250514` |
| `WithSystemPrompt(s)` | 系统提示词（纯字符串） | `""` |
| `WithClaudeCodePreset(append?)` | Claude Code 对齐的系统提示词 | — |
| `WithPromptBuilder(b)` | 结构化多段落 Prompt | `nil` |
| `WithAppendPrompt(s)` | 追加文本到系统提示词后 | `""` |
| `WithContextProviders(p...)` | 动态上下文注入 | `[]` |
| `WithMaxTokens(n)` | 每次调用最大输出 token | `16384` |
| `WithMaxTurns(n)` | 轮次上限（0=无限） | `0` |
| `WithTemperature(t)` | 采样温度 | `nil` |
| `WithTools(t...)` | 注册工具 | `[]` |
| `WithToolExecutor(e)` | 执行策略 | `ParallelExecutor` |
| `WithThinking(n)` | 扩展思维 token 预算 | `nil` |
| `WithPermissionHandler(h)` | 权限回调 | `nil`（全部允许） |
| `WithHooks(h)` | 生命周期钩子 | `nil` |
| `WithCostTracker(ct)` | 费用追踪器 | `nil` |
| `WithStore(s, id)` | 会话持久化 | `nil` |
| `WithCompact(n, opts...)` | 上下文压缩 | `nil` |

## 架构图

```
┌──────────────────────────────────────────────┐
│                 Agent (agent.go)             │  公共 API
│  Run / RunStream / RunMessages / Reset       │
├──────────────────────────────────────────────┤
│     PromptBuilder + ContextProviders         │  Prompt 组装
│  段落组装 → 缓存分界线 → 动态上下文注入       │  (Phase 5)
├──────────────────────────────────────────────┤
│              Agent Loop (loop.go)            │  核心循环
│  解析Prompt → 流式调用 → 权限检查 → 工具执行 → ↻ │
├─────────────────────┬────────────────────────┤
│   Provider (接口)   │   ToolExecutor (接口)  │  可替换接口
│   └→ claude/        │   └→ Parallel/Seq      │
├─────────────────────┼────────────────────────┤
│   Stream (接口)     │   Tool (接口)          │
│   └→ SSE stream     │   └→ 自定义工具         │
├─────────────────────┼────────────────────────┤
│   MCP Client        │   SubAgentTool         │  Phase 4
│   └→ 动态工具发现    │   └→ 任务委派           │
├─────────────────────┴────────────────────────┤
│          Message / ContentBlock / Usage      │  类型定义
└──────────────────────────────────────────────┘
```

## 示例

参见 [`examples/`](examples/) 目录：

- **[basic](examples/basic/)** — 基础对话
- **[tools](examples/tools/)** — 天气+时间工具，流式输出
- **[streaming](examples/streaming/)** — 实时事件处理

```bash
export ANTHROPIC_AUTH_TOKEN=sk-...
go run ./examples/basic
go run ./examples/tools
go run ./examples/streaming
```

## 开发路线

- [x] Phase 1: 核心 SDK — Agent 循环、Provider、Tool、流式响应
- [x] Phase 2: 内置工具 — Bash、文件读写、Glob、Grep
- [x] Phase 3: 高级功能 — 权限控制、钩子、费用追踪、会话持久化、上下文压缩
- [x] Phase 4: 生态扩展 — MCP Client、子 Agent、交互式权限
- [x] Phase 5: Prompt 工程 — PromptBuilder、缓存分界线、预设模板、ContextProviders
- [ ] 更多 Provider: OpenAI、Bedrock、Vertex
- [ ] Coordinator Mode: 多 Agent 编排

## 开发

```bash
make build            # 编译
make test             # 单元测试
make test-v           # 详细测试输出
make test-integration # 集成测试 (需设置 ANTHROPIC_AUTH_TOKEN)
make fmt              # 格式化
make vet              # 静态分析
make example-basic    # 运行基础示例
```

## 许可

MIT
