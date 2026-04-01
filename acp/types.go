// Package acp implements the Agent Client Protocol (ACP) server.
//
// ACP standardizes communication between code editors/IDEs and coding agents
// using JSON-RPC 2.0 over stdio. See https://agentclientprotocol.com
//
// This package bridges ACP to the go-agent-sdk Agent, allowing any
// ACP-compatible editor (Cursor, VS Code, etc.) to use the SDK as a backend.
package acp

import "encoding/json"

// ---------------------------------------------------------------------------
// JSON-RPC 2.0
// ---------------------------------------------------------------------------

type jsonrpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // nil for notifications
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

func (m *jsonrpcMessage) isRequest() bool      { return m.Method != "" && m.ID != nil }
func (m *jsonrpcMessage) isNotification() bool  { return m.Method != "" && m.ID == nil }
func (m *jsonrpcMessage) isResponse() bool      { return m.Method == "" && m.ID != nil }

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	errCodeParse          = -32700
	errCodeInvalidRequest = -32600
	errCodeMethodNotFound = -32601
	errCodeInvalidParams  = -32602
	errCodeInternal       = -32603
)

// ---------------------------------------------------------------------------
// ACP Protocol Version
// ---------------------------------------------------------------------------

const ProtocolVersion = 1

// ---------------------------------------------------------------------------
// initialize
// ---------------------------------------------------------------------------

type InitializeParams struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
	ClientInfo         *ImplementationInfo `json:"clientInfo,omitempty"`
}

type InitializeResult struct {
	ProtocolVersion   int                `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities  `json:"agentCapabilities"`
	AgentInfo         *ImplementationInfo `json:"agentInfo,omitempty"`
	AuthMethods       []AuthMethod       `json:"authMethods"`
}

type ImplementationInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

type AuthMethod struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// ---------------------------------------------------------------------------
// Capabilities
// ---------------------------------------------------------------------------

type ClientCapabilities struct {
	FS       *FSCapabilities `json:"fs,omitempty"`
	Terminal bool            `json:"terminal,omitempty"`
}

type FSCapabilities struct {
	ReadTextFile  bool `json:"readTextFile,omitempty"`
	WriteTextFile bool `json:"writeTextFile,omitempty"`
}

type AgentCapabilities struct {
	LoadSession         bool                `json:"loadSession,omitempty"`
	PromptCapabilities  *PromptCapabilities `json:"promptCapabilities,omitempty"`
	MCPCapabilities     *MCPCapabilities    `json:"mcpCapabilities,omitempty"`
	SessionCapabilities *SessionCaps        `json:"sessionCapabilities,omitempty"`
}

type PromptCapabilities struct {
	Image           bool `json:"image,omitempty"`
	Audio           bool `json:"audio,omitempty"`
	EmbeddedContext bool `json:"embeddedContext,omitempty"`
}

type MCPCapabilities struct {
	HTTP bool `json:"http,omitempty"`
	SSE  bool `json:"sse,omitempty"`
}

type SessionCaps struct {
	List bool `json:"list,omitempty"`
}

// ---------------------------------------------------------------------------
// session/new
// ---------------------------------------------------------------------------

type NewSessionParams struct {
	CWD        string      `json:"cwd"`
	MCPServers []MCPServer `json:"mcpServers"`
}

type NewSessionResult struct {
	SessionID string `json:"sessionId"`
}

type MCPServer struct {
	Name    string        `json:"name"`
	Command string        `json:"command,omitempty"`
	Args    []string      `json:"args,omitempty"`
	Env     []EnvVariable `json:"env,omitempty"`
	Type    string        `json:"type,omitempty"` // "stdio", "http", "sse"
	URL     string        `json:"url,omitempty"`
}

type EnvVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ---------------------------------------------------------------------------
// session/prompt
// ---------------------------------------------------------------------------

type PromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

type PromptResult struct {
	StopReason StopReason `json:"stopReason"`
}

type StopReason string

const (
	StopReasonEndTurn   StopReason = "end_turn"
	StopReasonMaxTokens StopReason = "max_tokens"
	StopReasonMaxTurns  StopReason = "max_turns"
	StopReasonRefusal   StopReason = "refusal"
	StopReasonCancelled StopReason = "cancelled"
)

// ---------------------------------------------------------------------------
// session/cancel
// ---------------------------------------------------------------------------

type CancelParams struct {
	SessionID string `json:"sessionId"`
}

// ---------------------------------------------------------------------------
// session/update — notification from agent to client
// ---------------------------------------------------------------------------

type SessionUpdateParams struct {
	SessionID string `json:"sessionId"`
	Update    any    `json:"update"` // one of the typed updates below
}

type MessageChunkUpdate struct {
	SessionUpdate string       `json:"sessionUpdate"` // agent_message_chunk, user_message_chunk, thought_message_chunk
	Content       ContentBlock `json:"content"`
}

type ToolCallNotification struct {
	SessionUpdate string          `json:"sessionUpdate"` // "tool_call"
	ToolCallID    string          `json:"toolCallId"`
	Title         string          `json:"title,omitempty"`
	Kind          string          `json:"kind,omitempty"`   // read, edit, execute, search, think, other
	Status        string          `json:"status,omitempty"` // pending, in_progress, completed, error
	RawInput      json.RawMessage `json:"rawInput,omitempty"`
}

type ToolCallStatusUpdate struct {
	SessionUpdate string            `json:"sessionUpdate"` // "tool_call_update"
	ToolCallID    string            `json:"toolCallId"`
	Status        string            `json:"status,omitempty"`
	Content       []ToolCallContent `json:"content,omitempty"`
	RawOutput     json.RawMessage   `json:"rawOutput,omitempty"`
}

type PlanUpdate struct {
	SessionUpdate string      `json:"sessionUpdate"` // "plan"
	Entries       []PlanEntry `json:"entries"`
}

type PlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority,omitempty"`
	Status   string `json:"status,omitempty"`
}

type ToolCallContent struct {
	Type    string        `json:"type"` // "content", "diff", "terminal"
	Content *ContentBlock `json:"content,omitempty"`
	Path    string        `json:"path,omitempty"`
	OldText *string       `json:"oldText,omitempty"`
	NewText *string       `json:"newText,omitempty"`
}

// ---------------------------------------------------------------------------
// session/request_permission — method from agent to client
// ---------------------------------------------------------------------------

type RequestPermissionParams struct {
	SessionID string             `json:"sessionId"`
	ToolCall  ToolCallUpdateData `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

type RequestPermissionResult struct {
	Outcome PermissionOutcome `json:"outcome"`
}

type PermissionOutcome struct {
	Outcome  string `json:"outcome"`           // "selected" or "cancelled"
	OptionID string `json:"optionId,omitempty"`
}

type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"` // allow_once, allow_always, reject_once, reject_always
}

type ToolCallUpdateData struct {
	ToolCallID string `json:"toolCallId"`
	Title      string `json:"title,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Status     string `json:"status,omitempty"`
}

// ---------------------------------------------------------------------------
// Content blocks (MCP-compatible)
// ---------------------------------------------------------------------------

type ContentBlock struct {
	Type     string            `json:"type"` // "text", "image", "resource", "resource_link"
	Text     string            `json:"text,omitempty"`
	MIMEType string            `json:"mimeType,omitempty"`
	Data     string            `json:"data,omitempty"` // base64 for image/audio
	Resource *EmbeddedResource `json:"resource,omitempty"`
	URI      string            `json:"uri,omitempty"`
	Name     string            `json:"name,omitempty"`
	Size     int64             `json:"size,omitempty"`
}

type EmbeddedResource struct {
	URI      string `json:"uri"`
	MIMEType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
}
