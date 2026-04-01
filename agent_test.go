package agentsdk

import (
	"context"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Agent creation
// ---------------------------------------------------------------------------

func TestNewAgent_NoProvider(t *testing.T) {
	_, err := New()
	if err == nil {
		t.Fatal("expected error when no provider is set")
	}
	if _, ok := err.(ErrNoProvider); !ok {
		t.Fatalf("expected ErrNoProvider, got %T: %v", err, err)
	}
}

func TestNewAgent_WithProvider(t *testing.T) {
	a, err := New(WithProvider(&mockProvider{responses: [][]StreamEvent{mockTextEvents("ok")}}))
	if err != nil {
		t.Fatal(err)
	}
	if a == nil {
		t.Fatal("agent should not be nil")
	}
}

// ---------------------------------------------------------------------------
// Simple text response
// ---------------------------------------------------------------------------

func TestRun_SimpleText(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		mockTextEvents("Hello! I'm an assistant."),
	}}
	a, err := New(WithProvider(p))
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Run(context.Background(), "Hi")
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != ReasonEndTurn {
		t.Fatalf("reason = %q, want %q", result.Reason, ReasonEndTurn)
	}

	msgs := a.Messages()
	if len(msgs) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(msgs))
	}
	if msgs[0].Role != RoleUser {
		t.Errorf("messages[0].Role = %q, want %q", msgs[0].Role, RoleUser)
	}
	if msgs[1].Role != RoleAssistant {
		t.Errorf("messages[1].Role = %q, want %q", msgs[1].Role, RoleAssistant)
	}
	if got := msgs[1].TextContent(); got != "Hello! I'm an assistant." {
		t.Errorf("response text = %q", got)
	}
}

// ---------------------------------------------------------------------------
// Tool call loop: tool_use → execute → final text
// ---------------------------------------------------------------------------

func TestRun_ToolCallLoop(t *testing.T) {
	tool := newMockTool("get_time", "Returns the current time", "2025-06-01 12:00:00 UTC")

	p := &mockProvider{responses: [][]StreamEvent{
		mockToolUseEvents("call_001", "get_time", `{}`),
		mockTextEvents("The current time is 2025-06-01 12:00:00 UTC."),
	}}

	a, err := New(WithProvider(p), WithTools(tool))
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Run(context.Background(), "What time is it?")
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != ReasonEndTurn {
		t.Fatalf("reason = %q, want %q", result.Reason, ReasonEndTurn)
	}
	if !tool.wasCalled() {
		t.Fatal("tool was not called")
	}

	msgs := a.Messages()
	// user → assistant(tool_use) → user(tool_result) → assistant(text)
	if len(msgs) != 4 {
		t.Fatalf("len(messages) = %d, want 4", len(msgs))
	}

	// Verify tool result was appended
	toolResultMsg := msgs[2]
	if toolResultMsg.Role != RoleUser {
		t.Errorf("messages[2].Role = %q, want user", toolResultMsg.Role)
	}
	if len(toolResultMsg.Content) == 0 || toolResultMsg.Content[0].Type != "tool_result" {
		t.Error("messages[2] should be a tool_result")
	}

	// Verify final answer
	if got := msgs[3].TextContent(); got != "The current time is 2025-06-01 12:00:00 UTC." {
		t.Errorf("final text = %q", got)
	}

	// Verify the second LLM call received the tool result in its messages
	recorded := p.recorded()
	if len(recorded) != 2 {
		t.Fatalf("provider called %d times, want 2", len(recorded))
	}
	secondCallMsgs := recorded[1].Messages
	found := false
	for _, m := range secondCallMsgs {
		for _, b := range m.Content {
			if b.Type == "tool_result" && b.ToolUseID == "call_001" {
				found = true
			}
		}
	}
	if !found {
		t.Error("second LLM call did not include tool_result for call_001")
	}
}

// ---------------------------------------------------------------------------
// Max turns
// ---------------------------------------------------------------------------

func TestRun_MaxTurns(t *testing.T) {
	tool := newMockTool("noop", "does nothing", "ok")

	p := &mockProvider{responses: [][]StreamEvent{
		mockToolUseEvents("c1", "noop", `{}`),
		// No second response needed; agent stops before second LLM call.
	}}

	a, err := New(WithProvider(p), WithTools(tool), WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Run(context.Background(), "do something")
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != ReasonMaxTurns {
		t.Fatalf("reason = %q, want %q", result.Reason, ReasonMaxTurns)
	}
}

// ---------------------------------------------------------------------------
// Streaming events
// ---------------------------------------------------------------------------

func TestRunStream_EmitsEvents(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		mockTextEvents("Hi there!"),
	}}
	a, err := New(WithProvider(p))
	if err != nil {
		t.Fatal(err)
	}

	var events []Event
	handler := func(evt Event) { events = append(events, evt) }

	_, err = a.RunStream(context.Background(), "Hello", handler)
	if err != nil {
		t.Fatal(err)
	}

	typeSeq := make([]EventType, len(events))
	for i, e := range events {
		typeSeq[i] = e.Type
	}

	// Expect at minimum: turn_start, text_delta, turn_end
	wantTypes := []EventType{EventTurnStart, EventTextDelta, EventTurnEnd}
	for _, want := range wantTypes {
		found := false
		for _, got := range typeSeq {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing event type %q in %v", want, typeSeq)
		}
	}
}

// ---------------------------------------------------------------------------
// Hooks
// ---------------------------------------------------------------------------

func TestHooks_FireInOrder(t *testing.T) {
	tool := newMockTool("echo", "echoes input", "echoed")

	var order []string
	hooks := &Hooks{
		BeforeTurn: func(_ context.Context, turn int, _ []Message) {
			order = append(order, "before_turn")
		},
		AfterTurn: func(_ context.Context, turn int, _ Usage) {
			order = append(order, "after_turn")
		},
		BeforeToolCall: func(_ context.Context, call ToolCall) error {
			order = append(order, "before_tool")
			return nil
		},
		AfterToolCall: func(_ context.Context, call ToolCall, _ ToolCallResult) {
			order = append(order, "after_tool")
		},
	}

	p := &mockProvider{responses: [][]StreamEvent{
		mockToolUseEvents("c1", "echo", `{}`),
		mockTextEvents("done"),
	}}

	a, err := New(WithProvider(p), WithTools(tool), WithHooks(hooks))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Run(context.Background(), "echo test"); err != nil {
		t.Fatal(err)
	}

	got := strings.Join(order, ",")
	// Turn 1: before_turn → LLM → after_turn → before_tool, after_tool
	// Turn 2: before_turn → LLM → after_turn
	want := "before_turn,after_turn,before_tool,after_tool,before_turn,after_turn"
	if got != want {
		t.Errorf("hook order:\n  got  = %s\n  want = %s", got, want)
	}
}

// ---------------------------------------------------------------------------
// Permission deny
// ---------------------------------------------------------------------------

func TestPermissionDeny_BlocksTool(t *testing.T) {
	tool := newMockTool("dangerous", "dangerous op", "should not run")

	p := &mockProvider{responses: [][]StreamEvent{
		mockToolUseEvents("c1", "dangerous", `{}`),
		mockTextEvents("OK, I won't do that."),
	}}

	a, err := New(
		WithProvider(p),
		WithTools(tool),
		WithPermissionHandler(DenyAll),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Run(context.Background(), "do dangerous thing")
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != ReasonEndTurn {
		t.Fatalf("reason = %q, want %q", result.Reason, ReasonEndTurn)
	}
	if tool.wasCalled() {
		t.Error("tool should NOT have been called when permission is denied")
	}

	// The second LLM call should see an error tool_result
	recorded := p.recorded()
	if len(recorded) < 2 {
		t.Fatalf("provider called %d times, want >= 2", len(recorded))
	}
	for _, m := range recorded[1].Messages {
		for _, b := range m.Content {
			if b.Type == "tool_result" && b.IsError && strings.Contains(b.Content, "permission denied") {
				return // success
			}
		}
	}
	t.Error("second LLM call did not include permission-denied tool_result")
}

// ---------------------------------------------------------------------------
// Usage tracking
// ---------------------------------------------------------------------------

func TestUsageTracking(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		mockTextEvents("hello"),
	}}
	a, err := New(WithProvider(p))
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage.InputTokens == 0 || result.Usage.OutputTokens == 0 {
		t.Errorf("usage should be non-zero: %+v", result.Usage)
	}
}

// ---------------------------------------------------------------------------
// Cost tracker
// ---------------------------------------------------------------------------

func TestCostTracker(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		mockTextEvents("hi"),
	}}
	tracker := NewCostTracker(nil)
	a, err := New(
		WithProvider(p),
		WithModel("claude-sonnet-4-20250514"),
		WithCostTracker(tracker),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Run(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if result.Cost <= 0 {
		t.Errorf("expected positive cost, got %f", result.Cost)
	}
	if tracker.TotalUsage().TotalTokens() == 0 {
		t.Error("tracker should have recorded tokens")
	}
}

// ---------------------------------------------------------------------------
// Reset clears state
// ---------------------------------------------------------------------------

func TestReset(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		mockTextEvents("first"),
		mockTextEvents("second"),
	}}
	a, err := New(WithProvider(p))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := a.Run(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	if len(a.Messages()) == 0 {
		t.Fatal("should have messages after first run")
	}

	a.Reset()
	if len(a.Messages()) != 0 {
		t.Error("messages should be empty after reset")
	}
}

// ---------------------------------------------------------------------------
// Concurrent run is rejected
// ---------------------------------------------------------------------------

func TestConcurrentRunRejected(t *testing.T) {
	started := make(chan struct{})
	blocker := make(chan struct{})

	p := &dynamicMockProvider{
		handler: func(idx int, _ *MessageParams) []StreamEvent {
			if idx == 0 {
				close(started)
				<-blocker
			}
			return mockTextEvents("ok")
		},
	}

	a, err := New(WithProvider(p))
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		_, _ = a.Run(context.Background(), "first")
	}()

	<-started // wait until first call is inside the provider

	_, err2 := a.Run(context.Background(), "second")
	close(blocker)

	if err2 == nil {
		t.Fatal("expected ErrAlreadyRunning, got nil")
	}
	if _, ok := err2.(ErrAlreadyRunning); !ok {
		t.Fatalf("expected ErrAlreadyRunning, got %T: %v", err2, err2)
	}
}

// ---------------------------------------------------------------------------
// Multiple tool calls in one turn
// ---------------------------------------------------------------------------

func TestRun_MultipleToolCalls(t *testing.T) {
	toolA := newMockTool("tool_a", "Tool A", "result_a")
	toolB := newMockTool("tool_b", "Tool B", "result_b")

	multiToolEvents := []StreamEvent{
		{Type: StreamEventMessageStart, Message: &MessageResponse{Usage: Usage{InputTokens: 10}}},
		{Type: StreamEventContentStart, Block: &ContentBlock{Type: "tool_use", ID: "c1", Name: "tool_a"}},
		{Type: StreamEventContentDelta, Delta: &Delta{Type: "input_json_delta", JSON: `{}`}},
		{Type: StreamEventContentStop},
		{Type: StreamEventContentStart, Block: &ContentBlock{Type: "tool_use", ID: "c2", Name: "tool_b"}},
		{Type: StreamEventContentDelta, Delta: &Delta{Type: "input_json_delta", JSON: `{}`}},
		{Type: StreamEventContentStop},
		{Type: StreamEventMessageDelta, Usage: &Usage{OutputTokens: 8}},
		{Type: StreamEventMessageStop},
	}

	p := &mockProvider{responses: [][]StreamEvent{
		multiToolEvents,
		mockTextEvents("Both tools returned results."),
	}}

	a, err := New(WithProvider(p), WithTools(toolA, toolB))
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Run(context.Background(), "use both tools")
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != ReasonEndTurn {
		t.Fatalf("reason = %q", result.Reason)
	}
	if !toolA.wasCalled() {
		t.Error("tool_a was not called")
	}
	if !toolB.wasCalled() {
		t.Error("tool_b was not called")
	}

	// Verify both tool results were sent back
	recorded := p.recorded()
	if len(recorded) != 2 {
		t.Fatalf("provider called %d times", len(recorded))
	}
	var resultIDs []string
	for _, m := range recorded[1].Messages {
		for _, b := range m.Content {
			if b.Type == "tool_result" {
				resultIDs = append(resultIDs, b.ToolUseID)
			}
		}
	}
	if len(resultIDs) != 2 {
		t.Fatalf("expected 2 tool_result blocks, got %d", len(resultIDs))
	}
}

// ---------------------------------------------------------------------------
// SetMessages / Messages snapshot
// ---------------------------------------------------------------------------

func TestSetMessages(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{mockTextEvents("ok")}}
	a, err := New(WithProvider(p))
	if err != nil {
		t.Fatal(err)
	}

	custom := []Message{NewUserMessage("injected")}
	a.SetMessages(custom)
	got := a.Messages()
	if len(got) != 1 || got[0].TextContent() != "injected" {
		t.Fatalf("SetMessages didn't work: %+v", got)
	}

	// Mutating the returned slice should not affect internal state.
	got[0] = NewUserMessage("mutated")
	if a.Messages()[0].TextContent() != "injected" {
		t.Error("Messages() should return a copy")
	}
}
