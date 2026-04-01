package agentsdk

import "encoding/json"

// Role represents the role of a message participant.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// StopReason indicates why the model stopped generating.
type StopReason string

const (
	StopReasonEndTurn      StopReason = "end_turn"
	StopReasonToolUse      StopReason = "tool_use"
	StopReasonMaxTokens    StopReason = "max_tokens"
	StopReasonStopSequence StopReason = "stop_sequence"
)

// Message represents a single conversation message exchanged with the LLM.
type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
}

// ContentBlock is a polymorphic content block within a message.
// The Type field determines which other fields are populated:
//
//   - "text":        Text
//   - "thinking":    Thinking
//   - "tool_use":    ID, Name, Input
//   - "tool_result": ToolUseID, Content, IsError
type ContentBlock struct {
	Type string `json:"type"`

	// --- text ---
	Text string `json:"text,omitempty"`

	// --- thinking ---
	Thinking string `json:"thinking,omitempty"`

	// --- tool_use ---
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// --- tool_result ---
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

// Usage tracks token consumption for a single API call.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// Add returns a new Usage that sums the token counts.
func (u Usage) Add(other Usage) Usage {
	return Usage{
		InputTokens:              u.InputTokens + other.InputTokens,
		OutputTokens:             u.OutputTokens + other.OutputTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens + other.CacheCreationInputTokens,
		CacheReadInputTokens:     u.CacheReadInputTokens + other.CacheReadInputTokens,
	}
}

// TotalTokens returns the sum of input and output tokens.
func (u Usage) TotalTokens() int {
	return u.InputTokens + u.OutputTokens
}

// ---------------------------------------------------------------------------
// Constructors
// ---------------------------------------------------------------------------

func NewTextBlock(text string) ContentBlock {
	return ContentBlock{Type: "text", Text: text}
}

func NewThinkingBlock(thinking string) ContentBlock {
	return ContentBlock{Type: "thinking", Thinking: thinking}
}

func NewToolUseBlock(id, name string, input json.RawMessage) ContentBlock {
	return ContentBlock{Type: "tool_use", ID: id, Name: name, Input: input}
}

func NewToolResultBlock(toolUseID, content string, isError bool) ContentBlock {
	return ContentBlock{Type: "tool_result", ToolUseID: toolUseID, Content: content, IsError: isError}
}

func NewUserMessage(text string) Message {
	return Message{
		Role:    RoleUser,
		Content: []ContentBlock{NewTextBlock(text)},
	}
}

func NewAssistantMessage(blocks ...ContentBlock) Message {
	return Message{Role: RoleAssistant, Content: blocks}
}

func NewToolResultMessage(results ...ContentBlock) Message {
	return Message{Role: RoleUser, Content: results}
}

// TextContent returns the concatenated text from all text blocks.
func (m Message) TextContent() string {
	var buf []byte
	for _, b := range m.Content {
		if b.Type == "text" && b.Text != "" {
			if len(buf) > 0 {
				buf = append(buf, '\n')
			}
			buf = append(buf, b.Text...)
		}
	}
	return string(buf)
}

// ToolUseBlocks returns all tool_use content blocks from the message.
func (m Message) ToolUseBlocks() []ContentBlock {
	var out []ContentBlock
	for _, b := range m.Content {
		if b.Type == "tool_use" {
			out = append(out, b)
		}
	}
	return out
}
