package agentsdk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// ---------------------------------------------------------------------------
// Mock Provider
// ---------------------------------------------------------------------------

type mockProvider struct {
	mu        sync.Mutex
	calls     []*MessageParams
	responses [][]StreamEvent
}

func (p *mockProvider) CreateMessage(_ context.Context, params *MessageParams) (*MessageResponse, error) {
	return nil, fmt.Errorf("mock: CreateMessage not implemented")
}

func (p *mockProvider) CreateMessageStream(_ context.Context, params *MessageParams) (Stream, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	cp := *params
	p.calls = append(p.calls, &cp)

	idx := len(p.calls) - 1
	if idx >= len(p.responses) {
		return nil, fmt.Errorf("mock: no response for call %d (have %d)", idx, len(p.responses))
	}
	return &mockStream{events: p.responses[idx]}, nil
}

func (p *mockProvider) recorded() []*MessageParams {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// ---------------------------------------------------------------------------
// Mock Stream
// ---------------------------------------------------------------------------

type mockStream struct {
	events []StreamEvent
	idx    int
	mu     sync.Mutex
}

func (s *mockStream) Recv() (StreamEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx >= len(s.events) {
		return StreamEvent{}, io.EOF
	}
	evt := s.events[s.idx]
	s.idx++
	return evt, nil
}

func (s *mockStream) Close() error { return nil }

// ---------------------------------------------------------------------------
// Mock Tool
// ---------------------------------------------------------------------------

type mockTool struct {
	spec     ToolSpec
	result   *ToolResult
	mu       sync.Mutex
	called   bool
	lastCall ToolCall
}

func (t *mockTool) Definition() ToolSpec { return t.spec }

func (t *mockTool) Execute(_ context.Context, call ToolCall) (*ToolResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.called = true
	t.lastCall = call
	return t.result, nil
}

func (t *mockTool) wasCalled() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.called
}

// ---------------------------------------------------------------------------
// Event sequence builders
// ---------------------------------------------------------------------------

func mockTextEvents(text string) []StreamEvent {
	return []StreamEvent{
		{Type: StreamEventMessageStart, Message: &MessageResponse{Usage: Usage{InputTokens: 10}}},
		{Type: StreamEventContentStart, Block: &ContentBlock{Type: "text"}},
		{Type: StreamEventContentDelta, Delta: &Delta{Type: "text_delta", Text: text}},
		{Type: StreamEventContentStop},
		{Type: StreamEventMessageDelta, Usage: &Usage{OutputTokens: 5}},
		{Type: StreamEventMessageStop},
	}
}

func mockToolUseEvents(id, name string, inputJSON string) []StreamEvent {
	return []StreamEvent{
		{Type: StreamEventMessageStart, Message: &MessageResponse{Usage: Usage{InputTokens: 10}}},
		{Type: StreamEventContentStart, Block: &ContentBlock{Type: "tool_use", ID: id, Name: name}},
		{Type: StreamEventContentDelta, Delta: &Delta{Type: "input_json_delta", JSON: inputJSON}},
		{Type: StreamEventContentStop},
		{Type: StreamEventMessageDelta, Usage: &Usage{OutputTokens: 5}},
		{Type: StreamEventMessageStop},
	}
}

func newMockTool(name, desc string, result string) *mockTool {
	return &mockTool{
		spec: ToolSpec{
			Name:        name,
			Description: desc,
			InputSchema: &JSONSchema{
				Type:       "object",
				Properties: map[string]*JSONSchema{},
			},
		},
		result: &ToolResult{Content: result},
	}
}

func newMockToolWithSchema(name, desc string, schema *JSONSchema, result string) *mockTool {
	return &mockTool{
		spec: ToolSpec{
			Name:        name,
			Description: desc,
			InputSchema: schema,
		},
		result: &ToolResult{Content: result},
	}
}

// dynamicMockProvider lets a callback decide the response per call.
type dynamicMockProvider struct {
	mu      sync.Mutex
	calls   []*MessageParams
	handler func(idx int, params *MessageParams) []StreamEvent
}

func (p *dynamicMockProvider) CreateMessage(_ context.Context, _ *MessageParams) (*MessageResponse, error) {
	return nil, fmt.Errorf("mock: CreateMessage not implemented")
}

func (p *dynamicMockProvider) CreateMessageStream(_ context.Context, params *MessageParams) (Stream, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	cp := *params
	p.calls = append(p.calls, &cp)
	idx := len(p.calls) - 1

	events := p.handler(idx, &cp)
	if events == nil {
		return nil, fmt.Errorf("mock: handler returned nil for call %d", idx)
	}
	return &mockStream{events: events}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
