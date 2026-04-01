// Package agentsdk provides a composable Go SDK for building LLM-powered agents.
//
// The SDK separates concerns into swappable interfaces:
//
//   - [Provider]     — LLM backend (Anthropic, Bedrock, Vertex, etc.)
//   - [Tool]         — executable capability exposed to the model
//   - [ToolExecutor] — strategy for dispatching tool calls (parallel / sequential)
//   - [Stream]       — incremental response delivery
//
// A minimal agent requires only a Provider:
//
//	import (
//	    agentsdk "github.com/chenhg5/go-agent-sdk"
//	    "github.com/chenhg5/go-agent-sdk/claude"
//	)
//
//	agent, _ := agentsdk.New(
//	    agentsdk.WithProvider(claude.NewProvider("sk-ant-...")),
//	)
//	result, _ := agent.Run(ctx, "Hello!")
//	fmt.Println(result.Messages[len(result.Messages)-1].TextContent())
package agentsdk
