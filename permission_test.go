package agentsdk

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestChannelPermission(t *testing.T) {
	requests := make(chan PermissionRequest, 1)
	responses := make(chan PermissionResponse, 1)

	tool := newMockTool("risky", "risky op", "done")
	p := &mockProvider{responses: [][]StreamEvent{
		mockToolUseEvents("c1", "risky", `{"action":"delete"}`),
		mockTextEvents("OK, skipped."),
	}}

	a, err := New(
		WithProvider(p),
		WithTools(tool),
		WithPermissionHandler(ChannelPermission(requests, responses)),
	)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		req := <-requests
		if req.Call.Name != "risky" {
			t.Errorf("expected tool name 'risky', got %q", req.Call.Name)
		}
		responses <- PermissionResponse{Decision: PermissionDeny, Reason: "user said no"}
	}()

	result, err := a.Run(context.Background(), "do risky thing")
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != ReasonEndTurn {
		t.Fatalf("reason = %q", result.Reason)
	}
	if tool.wasCalled() {
		t.Error("tool should NOT have been called when denied via channel")
	}
}

func TestChannelPermission_Allow(t *testing.T) {
	requests := make(chan PermissionRequest, 1)
	responses := make(chan PermissionResponse, 1)

	tool := newMockTool("safe", "safe op", "ok")
	p := &mockProvider{responses: [][]StreamEvent{
		mockToolUseEvents("c1", "safe", `{}`),
		mockTextEvents("done"),
	}}

	a, err := New(
		WithProvider(p),
		WithTools(tool),
		WithPermissionHandler(ChannelPermission(requests, responses)),
	)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		<-requests
		responses <- PermissionResponse{Decision: PermissionAllow}
	}()

	_, err = a.Run(context.Background(), "do safe thing")
	if err != nil {
		t.Fatal(err)
	}
	if !tool.wasCalled() {
		t.Error("tool should have been called when allowed via channel")
	}
}

func TestModifiedInput(t *testing.T) {
	tool := newMockToolWithSchema("exec", "execute command", &JSONSchema{
		Type: "object",
		Properties: map[string]*JSONSchema{
			"cmd": {Type: "string"},
		},
	}, "executed")

	p := &mockProvider{responses: [][]StreamEvent{
		mockToolUseEvents("c1", "exec", `{"cmd":"rm -rf /"}`),
		mockTextEvents("done"),
	}}

	sanitized := json.RawMessage(`{"cmd":"echo safe"}`)

	a, err := New(
		WithProvider(p),
		WithTools(tool),
		WithPermissionHandler(func(_ context.Context, req PermissionRequest) PermissionResponse {
			return PermissionResponse{
				Decision:      PermissionAllow,
				ModifiedInput: sanitized,
			}
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "run command")
	if err != nil {
		t.Fatal(err)
	}

	if !tool.wasCalled() {
		t.Fatal("tool was not called")
	}
	if string(tool.lastCall.Input) != string(sanitized) {
		t.Errorf("input was not modified: got %s, want %s", tool.lastCall.Input, sanitized)
	}
}

func TestPermissionEvents(t *testing.T) {
	tool := newMockTool("op", "operation", "result")
	p := &mockProvider{responses: [][]StreamEvent{
		mockToolUseEvents("c1", "op", `{}`),
		mockTextEvents("done"),
	}}

	a, err := New(WithProvider(p), WithTools(tool))
	if err != nil {
		t.Fatal(err)
	}

	var events []Event
	handler := func(evt Event) { events = append(events, evt) }

	_, err = a.RunStream(context.Background(), "test", handler)
	if err != nil {
		t.Fatal(err)
	}

	var permReqs, permResults int
	for _, evt := range events {
		switch evt.Type {
		case EventPermissionRequest:
			permReqs++
			if evt.Permission == nil || evt.Permission.ToolName != "op" {
				t.Error("permission request event missing tool info")
			}
		case EventPermissionResult:
			permResults++
		}
	}
	if permReqs != 1 {
		t.Errorf("expected 1 permission request event, got %d", permReqs)
	}
	if permResults != 1 {
		t.Errorf("expected 1 permission result event, got %d", permResults)
	}
}

func TestWithToolCheckerAndPrompter(t *testing.T) {
	reg := NewToolRegistry()

	writeToolImpl := &mockToolWithPerm{
		mockTool: mockTool{
			spec:   ToolSpec{Name: "write_file", Description: "write"},
			result: &ToolResult{Content: "written"},
		},
		permDecision: PermissionAsk,
	}
	readToolImpl := &mockToolWithPerm{
		mockTool: mockTool{
			spec:   ToolSpec{Name: "read_file", Description: "read"},
			result: &ToolResult{Content: "content"},
		},
		permDecision: PermissionAllow,
	}

	reg.Register(writeToolImpl)
	reg.Register(readToolImpl)

	var prompted bool
	prompter := func(_ context.Context, req PermissionRequest) PermissionResponse {
		prompted = true
		return PermissionResponse{Decision: PermissionDeny, Reason: "user rejected write"}
	}

	handler := WithToolCheckerAndPrompter(reg, prompter)

	// Read tool: checker returns Allow → no prompter call
	resp := handler(context.Background(), PermissionRequest{
		Tool: readToolImpl.Definition(),
		Call: ToolCall{Name: "read_file"},
	})
	if resp.Decision != PermissionAllow {
		t.Errorf("read_file should be allowed, got %v", resp.Decision)
	}
	if prompted {
		t.Error("prompter should not be called for allowed tools")
	}

	// Write tool: checker returns Ask → prompter called → denied
	resp = handler(context.Background(), PermissionRequest{
		Tool: writeToolImpl.Definition(),
		Call: ToolCall{Name: "write_file"},
	})
	if resp.Decision != PermissionDeny {
		t.Errorf("write_file should be denied after prompt, got %v", resp.Decision)
	}
	if !prompted {
		t.Error("prompter should have been called for Ask decision")
	}
	if !strings.Contains(resp.Reason, "user rejected") {
		t.Errorf("reason should mention user rejection: %q", resp.Reason)
	}
}

// mockToolWithPerm implements both Tool and ToolPermChecker.
type mockToolWithPerm struct {
	mockTool
	permDecision PermissionDecision
}

func (t *mockToolWithPerm) CheckPermission(_ ToolCall) (PermissionDecision, error) {
	return t.permDecision, nil
}
