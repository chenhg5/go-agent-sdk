package agentsdk

import (
	"context"
	"encoding/json"
)

// Tool is the interface every tool (built-in or custom) must implement.
type Tool interface {
	// Definition returns the tool specification sent to the LLM.
	Definition() ToolSpec
	// Execute runs the tool with the given call parameters.
	Execute(ctx context.Context, call ToolCall) (*ToolResult, error)
}

// ToolValidator is an optional interface a Tool may implement to reject
// invalid input before execution.
type ToolValidator interface {
	ValidateInput(input json.RawMessage) error
}

// ToolPrompter is an optional interface a Tool may implement to provide
// a rich, dynamic description for the LLM API.
//
// When a tool implements ToolPrompter, its Prompt() output is used as the
// API-level tool description instead of the static Description from Definition().
// This mirrors Claude Code's pattern where each tool's prompt() method returns
// detailed usage instructions, best practices, and cross-references to other tools.
//
// The PromptContext provides information about the current tool pool and
// configuration, allowing the prompt to reference other registered tools.
type ToolPrompter interface {
	Prompt(ctx PromptContext) string
}

// PromptContext provides context for dynamic tool prompt generation.
type PromptContext struct {
	// Tools lists all registered tool names, allowing cross-references
	// like "use Read instead of cat via Bash".
	Tools []string
	// Model is the current model name, for model-specific instructions.
	Model string
	// CWD is the current working directory.
	CWD string
}

// ToolPermChecker is an optional interface a Tool may implement to check
// permissions before execution.
type ToolPermChecker interface {
	CheckPermission(call ToolCall) (PermissionDecision, error)
}

// PermissionDecision indicates whether the tool call is allowed.
type PermissionDecision int

const (
	PermissionAllow PermissionDecision = iota
	PermissionDeny
	PermissionAsk
)

// ---------------------------------------------------------------------------
// Tool specification (sent to the LLM)
// ---------------------------------------------------------------------------

// ToolSpec is the JSON-serialisable tool definition for the Messages API.
type ToolSpec struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema *JSONSchema `json:"input_schema"`
}

// JSONSchema is a minimal representation of JSON Schema used for tool input.
type JSONSchema struct {
	Type                 string                 `json:"type"`
	Properties           map[string]*JSONSchema `json:"properties,omitempty"`
	Required             []string               `json:"required,omitempty"`
	Description          string                 `json:"description,omitempty"`
	Enum                 []string               `json:"enum,omitempty"`
	Items                *JSONSchema            `json:"items,omitempty"`
	Default              any                    `json:"default,omitempty"`
	AdditionalProperties *bool                  `json:"additionalProperties,omitempty"`
}

// ---------------------------------------------------------------------------
// Tool call / result (runtime)
// ---------------------------------------------------------------------------

// ToolCall represents a pending invocation requested by the LLM.
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ToolResult is the output produced by Tool.Execute.
type ToolResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

// ToolCallResult pairs a tool-use ID with its execution output.
type ToolCallResult struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}
