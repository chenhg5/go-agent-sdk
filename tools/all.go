// Package tools provides built-in tool implementations for the agent SDK.
//
// All tools implement the agentsdk.Tool interface and can be registered
// individually or as a group via DefaultTools / RegisterAll.
package tools

import agentsdk "github.com/chenhg5/go-agent-sdk"

// DefaultTools returns a slice of all built-in tools with default settings.
func DefaultTools() []agentsdk.Tool {
	return []agentsdk.Tool{
		&BashTool{},
		&FileReadTool{},
		&FileEditTool{},
		&FileWriteTool{},
		&GlobTool{},
		&GrepTool{},
	}
}

// RegisterAll adds all built-in tools to the given registry.
func RegisterAll(r *agentsdk.ToolRegistry) {
	for _, t := range DefaultTools() {
		r.Register(t)
	}
}
