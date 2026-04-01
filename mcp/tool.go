package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agentsdk "github.com/chenhg5/go-agent-sdk"
)

// MCPTool wraps a single MCP server tool as an agentsdk.Tool.
type MCPTool struct {
	client *Client
	info   ToolInfo
}

var _ agentsdk.Tool = (*MCPTool)(nil)

func (t *MCPTool) Definition() agentsdk.ToolSpec {
	spec := agentsdk.ToolSpec{
		Name:        t.info.Name,
		Description: t.info.Description,
	}
	if len(t.info.InputSchema) > 0 {
		var schema agentsdk.JSONSchema
		if json.Unmarshal(t.info.InputSchema, &schema) == nil {
			spec.InputSchema = &schema
		}
	}
	return spec
}

func (t *MCPTool) Execute(ctx context.Context, call agentsdk.ToolCall) (*agentsdk.ToolResult, error) {
	result, err := t.client.CallTool(ctx, call.Name, call.Input)
	if err != nil {
		return &agentsdk.ToolResult{
			Content: fmt.Sprintf("MCP tool error: %v", err),
			IsError: true,
		}, nil
	}

	var sb strings.Builder
	for i, c := range result.Content {
		if i > 0 {
			sb.WriteByte('\n')
		}
		switch c.Type {
		case "text":
			sb.WriteString(c.Text)
		default:
			sb.WriteString(fmt.Sprintf("[%s content]", c.Type))
		}
	}

	return &agentsdk.ToolResult{
		Content: sb.String(),
		IsError: result.IsError,
	}, nil
}

// ToolsFromClient discovers all tools from an MCP server and returns them
// as agentsdk.Tool instances ready for use with WithTools.
//
//	mcpTools, err := mcp.ToolsFromClient(client)
//	agent, _ := agentsdk.New(agentsdk.WithTools(mcpTools...))
func ToolsFromClient(client *Client) ([]agentsdk.Tool, error) {
	infos, err := client.ListTools(context.Background())
	if err != nil {
		return nil, err
	}
	tools := make([]agentsdk.Tool, len(infos))
	for i, info := range infos {
		tools[i] = &MCPTool{client: client, info: info}
	}
	return tools, nil
}
