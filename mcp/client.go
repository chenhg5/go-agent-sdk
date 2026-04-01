package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
)

const protocolVersion = "2024-11-05"

// Client is a high-level MCP client that wraps a Transport.
type Client struct {
	transport  Transport
	nextID     atomic.Int64
	serverInfo *ServerInfo
}

// NewClient creates an MCP client over the given transport and performs the
// initialize + initialized handshake.
func NewClient(ctx context.Context, transport Transport) (*Client, error) {
	c := &Client{transport: transport}
	if err := c.initialize(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// NewStdioClient is a convenience that starts a subprocess MCP server and
// creates a fully initialised Client.
//
//	client, err := mcp.NewStdioClient(ctx, "npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp")
func NewStdioClient(ctx context.Context, name string, args ...string) (*Client, error) {
	transport, err := NewStdioTransport(ctx, name, args...)
	if err != nil {
		return nil, err
	}
	client, err := NewClient(ctx, transport)
	if err != nil {
		transport.Close()
		return nil, err
	}
	return client, nil
}

// ServerInfo returns the server's info from the initialize handshake.
func (c *Client) ServerInfo() *ServerInfo {
	return c.serverInfo
}

// ListTools returns all tools the MCP server exposes.
func (c *Client) ListTools(ctx context.Context) ([]ToolInfo, error) {
	raw, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: tools/list: %w", err)
	}
	var result toolsListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("mcp: decode tools/list: %w", err)
	}
	return result.Tools, nil
}

// CallTool invokes a tool by name with the given JSON arguments.
func (c *Client) CallTool(ctx context.Context, name string, arguments json.RawMessage) (*ToolCallResult, error) {
	raw, err := c.call(ctx, "tools/call", toolCallParams{Name: name, Arguments: arguments})
	if err != nil {
		return nil, fmt.Errorf("mcp: tools/call %q: %w", name, err)
	}
	var result ToolCallResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("mcp: decode tools/call: %w", err)
	}
	return &result, nil
}

// ListResources returns all resources the MCP server exposes.
func (c *Client) ListResources(ctx context.Context) ([]ResourceInfo, error) {
	raw, err := c.call(ctx, "resources/list", nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: resources/list: %w", err)
	}
	var result resourcesListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("mcp: decode resources/list: %w", err)
	}
	return result.Resources, nil
}

// ReadResource reads a resource by URI.
func (c *Client) ReadResource(ctx context.Context, uri string) ([]ResourceContent, error) {
	raw, err := c.call(ctx, "resources/read", resourceReadParams{URI: uri})
	if err != nil {
		return nil, fmt.Errorf("mcp: resources/read %q: %w", uri, err)
	}
	var result resourceReadResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("mcp: decode resources/read: %w", err)
	}
	return result.Contents, nil
}

// Close shuts down the client and its transport.
func (c *Client) Close() error {
	return c.transport.Close()
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

func (c *Client) initialize(ctx context.Context) error {
	raw, err := c.call(ctx, "initialize", initializeParams{
		ProtocolVersion: protocolVersion,
		ClientInfo:      clientInfo{Name: "go-agent-sdk", Version: "1.0.0"},
	})
	if err != nil {
		return fmt.Errorf("mcp: initialize: %w", err)
	}

	var result initializeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("mcp: decode initialize: %w", err)
	}
	c.serverInfo = &result.ServerInfo

	// Send "initialized" notification to complete the handshake.
	if err := c.transport.Notify(ctx, "notifications/initialized", nil); err != nil {
		return fmt.Errorf("mcp: initialized notification: %w", err)
	}
	return nil
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	req := &jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      c.nextID.Add(1),
		Method:  method,
		Params:  params,
	}

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	return resp.Result, nil
}
