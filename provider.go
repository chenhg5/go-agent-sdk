package agentsdk

import (
	"context"
	"io"
)

// Provider abstracts the LLM backend (Anthropic, Bedrock, Vertex, etc.).
// Implement this interface to plug in a different model provider.
type Provider interface {
	// CreateMessage sends a non-streaming request and returns the full response.
	CreateMessage(ctx context.Context, params *MessageParams) (*MessageResponse, error)
	// CreateMessageStream sends a streaming request and returns a Stream.
	CreateMessageStream(ctx context.Context, params *MessageParams) (Stream, error)
}

// Stream delivers server-sent events from a streaming LLM response.
// Implementations must be safe to call Recv from a single goroutine.
type Stream interface {
	// Recv returns the next event. Returns io.EOF when the stream ends normally.
	Recv() (StreamEvent, error)
	// Close releases the underlying resources. Safe to call multiple times.
	Close() error
}

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

// MessageParams contains everything needed to call the Messages API.
type MessageParams struct {
	Model         string          `json:"model"`
	Messages      []Message       `json:"messages"`
	System        string          `json:"system,omitempty"`
	Tools         []ToolSpec      `json:"tools,omitempty"`
	MaxTokens     int             `json:"max_tokens"`
	Temperature   *float64        `json:"temperature,omitempty"`
	TopP          *float64        `json:"top_p,omitempty"`
	TopK          *int            `json:"top_k,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	ToolChoice    *ToolChoice     `json:"tool_choice,omitempty"`
	Thinking      *ThinkingConfig `json:"thinking,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
}

// ThinkingConfig enables extended thinking.
type ThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

// ToolChoice controls how the model selects tools.
type ToolChoice struct {
	Type string `json:"type"`           // "auto", "any", "tool"
	Name string `json:"name,omitempty"` // required when Type == "tool"
}

// MessageResponse is the full (non-streaming) response from the Messages API.
type MessageResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       Role           `json:"role"`
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model"`
	StopReason StopReason     `json:"stop_reason"`
	Usage      Usage          `json:"usage"`
}

// ---------------------------------------------------------------------------
// Stream event types
// ---------------------------------------------------------------------------

// StreamEventType classifies the kind of server-sent event.
type StreamEventType string

const (
	StreamEventMessageStart StreamEventType = "message_start"
	StreamEventContentStart StreamEventType = "content_block_start"
	StreamEventContentDelta StreamEventType = "content_block_delta"
	StreamEventContentStop  StreamEventType = "content_block_stop"
	StreamEventMessageDelta StreamEventType = "message_delta"
	StreamEventMessageStop  StreamEventType = "message_stop"
	StreamEventPing         StreamEventType = "ping"
)

// StreamEvent is a single parsed event from the streaming response.
type StreamEvent struct {
	Type  StreamEventType

	Index   int              // content-block index (content_* events)
	Message *MessageResponse // partial message (message_start)
	Block   *ContentBlock    // initial block (content_block_start)
	Delta   *Delta           // incremental payload (content_block_delta)

	StopReason StopReason // message_delta
	Usage      *Usage     // message_start or message_delta
}

// Delta carries an incremental content update.
type Delta struct {
	Type     string `json:"type"` // "text_delta", "input_json_delta", "thinking_delta"
	Text     string `json:"text,omitempty"`
	JSON     string `json:"partial_json,omitempty"`
	Thinking string `json:"thinking,omitempty"`
}

// compile-time guard
var _ Stream = (*nopStream)(nil)

// nopStream is a placeholder used by default-value paths.
type nopStream struct{}

func (nopStream) Recv() (StreamEvent, error) { return StreamEvent{}, io.EOF }
func (nopStream) Close() error               { return nil }
