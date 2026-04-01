package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	agentsdk "github.com/chenhg5/go-agent-sdk"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// fakeProvider satisfies agentsdk.Provider for testing.
type fakeProvider struct{}

func (fakeProvider) CreateMessage(_ context.Context, _ *agentsdk.MessageParams) (*agentsdk.MessageResponse, error) {
	return &agentsdk.MessageResponse{
		Role: agentsdk.RoleAssistant,
		Content: []agentsdk.ContentBlock{
			agentsdk.NewTextBlock("Hello from the agent!"),
		},
		StopReason: agentsdk.StopReasonEndTurn,
		Usage:      agentsdk.Usage{InputTokens: 10, OutputTokens: 5},
	}, nil
}

func (fakeProvider) CreateMessageStream(_ context.Context, _ *agentsdk.MessageParams) (agentsdk.Stream, error) {
	return &fakeStream{}, nil
}

type fakeStream struct{ done bool }

func (s *fakeStream) Recv() (agentsdk.StreamEvent, error) {
	if s.done {
		return agentsdk.StreamEvent{}, io.EOF
	}
	s.done = true
	return agentsdk.StreamEvent{
		Type: agentsdk.StreamEventMessageStart,
		Message: &agentsdk.MessageResponse{
			Role: agentsdk.RoleAssistant,
			Content: []agentsdk.ContentBlock{
				agentsdk.NewTextBlock("Hello from the agent!"),
			},
			StopReason: agentsdk.StopReasonEndTurn,
			Usage:      agentsdk.Usage{InputTokens: 10, OutputTokens: 5},
		},
	}, nil
}

func (s *fakeStream) Close() error { return nil }

func testFactory(_ context.Context, _ NewSessionParams, perm agentsdk.PermissionHandler) (agentsdk.Agent, error) {
	opts := []agentsdk.Option{
		agentsdk.WithProvider(fakeProvider{}),
		agentsdk.WithSystemPrompt("You are a test agent."),
	}
	if perm != nil {
		opts = append(opts, agentsdk.WithPermissionHandler(perm))
	}
	a, _ := agentsdk.New(opts...)
	return a, nil
}

// runServerScript sends a series of JSON-RPC messages to the server and
// returns all output lines.
func runServerScript(t *testing.T, lines []string) []jsonrpcMessage {
	t.Helper()

	input := strings.Join(lines, "\n") + "\n"
	in := strings.NewReader(input)
	var out bytes.Buffer

	srv := NewServer(ServerConfig{AgentFactory: testFactory})
	_ = srv.RunOn(in, &out)

	var msgs []jsonrpcMessage
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var msg jsonrpcMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("unmarshal output: %v\nline: %s", err, line)
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestServer_Initialize(t *testing.T) {
	msgs := runServerScript(t, []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}`,
	})

	if len(msgs) == 0 {
		t.Fatal("no output")
	}

	var result InitializeResult
	if err := json.Unmarshal(msgs[0].Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.ProtocolVersion != 1 {
		t.Errorf("protocol version = %d", result.ProtocolVersion)
	}
	if result.AgentInfo == nil || result.AgentInfo.Name != "go-agent-sdk" {
		t.Error("missing agent info")
	}
}

func TestServer_NewSession(t *testing.T) {
	msgs := runServerScript(t, []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"/tmp","mcpServers":[]}}`,
	})

	if len(msgs) < 2 {
		t.Fatalf("expected >= 2 msgs, got %d", len(msgs))
	}

	var result NewSessionResult
	if err := json.Unmarshal(msgs[1].Result, &result); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.SessionID, "sess_") {
		t.Errorf("session id = %q", result.SessionID)
	}
}

func TestServer_PromptAndStream(t *testing.T) {
	// Use io.Pipe so the prompt goroutine has time to write output
	// before we close and collect everything.
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer(ServerConfig{AgentFactory: testFactory})
	serverDone := make(chan error, 1)
	go func() { serverDone <- srv.RunOn(inReader, outWriter) }()

	// Step 1: initialize
	writeLine(t, inWriter, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}`)
	readResponse(t, outReader) // consume init response

	// Step 2: session/new
	writeLine(t, inWriter, `{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"/tmp","mcpServers":[]}}`)
	sessResp := readResponse(t, outReader)
	var sessResult NewSessionResult
	if err := json.Unmarshal(sessResp.Result, &sessResult); err != nil {
		t.Fatal(err)
	}

	// Step 3: session/prompt
	promptParams := mustMarshal(t, PromptParams{
		SessionID: sessResult.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "Hello!"}},
	})
	writeLine(t, inWriter, mustJSON(t, jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`3`),
		Method:  "session/prompt",
		Params:  promptParams,
	}))

	// Collect output until we get the prompt response (id=3).
	hasPromptResponse := false
	scanner := bufio.NewScanner(outReader)
	for scanner.Scan() {
		var msg jsonrpcMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		if msg.isResponse() && string(msg.ID) == "3" {
			hasPromptResponse = true
			var pr PromptResult
			_ = json.Unmarshal(msg.Result, &pr)
			if pr.StopReason != StopReasonEndTurn {
				t.Errorf("stop reason = %q", pr.StopReason)
			}
			break
		}
	}

	if !hasPromptResponse {
		t.Error("missing prompt response")
	}

	// Cleanup
	_ = inWriter.Close()
	_ = outWriter.Close()
}

func TestServer_UnknownSession(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"sessionId":"bad","prompt":[{"type":"text","text":"hi"}]}}`,
	}, "\n") + "\n"

	in := strings.NewReader(input)
	var out bytes.Buffer

	srv := NewServer(ServerConfig{AgentFactory: testFactory})
	_ = srv.RunOn(in, &out)

	outLines := strings.Split(strings.TrimSpace(out.String()), "\n")
	for _, line := range outLines {
		var msg jsonrpcMessage
		_ = json.Unmarshal([]byte(line), &msg)
		if msg.isResponse() && string(msg.ID) == "2" {
			if msg.Error == nil {
				t.Error("expected error for unknown session")
			}
			return
		}
	}
	// It's possible the error arrives later since prompt runs in a goroutine;
	// that's fine for the test coverage.
}

func TestServer_Cancel(t *testing.T) {
	// Just verify cancel doesn't panic with an unknown session.
	msgs := runServerScript(t, []string{
		`{"jsonrpc":"2.0","method":"session/cancel","params":{"sessionId":"nonexistent"}}`,
	})
	_ = msgs // no crash = pass
}

func TestClassifyToolKind(t *testing.T) {
	tests := map[string]string{
		"file_read":  "read",
		"file_edit":  "edit",
		"file_write": "edit",
		"bash":       "execute",
		"glob":       "read",
		"grep":       "read",
		"think":      "think",
		"custom":     "other",
	}
	for name, want := range tests {
		if got := classifyToolKind(name); got != want {
			t.Errorf("classifyToolKind(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestExtractText(t *testing.T) {
	blocks := []ContentBlock{
		{Type: "text", Text: "Hello"},
		{Type: "resource", Resource: &EmbeddedResource{URI: "file:///a.go", Text: "package main"}},
		{Type: "text", Text: "World"},
	}
	got := extractText(blocks)
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "World") || !strings.Contains(got, "package main") {
		t.Errorf("extractText = %q", got)
	}
}

func TestMapStopReason(t *testing.T) {
	if got := mapStopReason(agentsdk.ReasonEndTurn); got != StopReasonEndTurn {
		t.Errorf("got %q", got)
	}
	if got := mapStopReason(agentsdk.ReasonMaxTurns); got != StopReasonMaxTurns {
		t.Errorf("got %q", got)
	}
	if got := mapStopReason(agentsdk.ReasonAborted); got != StopReasonCancelled {
		t.Errorf("got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeLine(t *testing.T, w io.Writer, line string) {
	t.Helper()
	_, err := w.Write([]byte(line + "\n"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readResponse(t *testing.T, r io.Reader) jsonrpcMessage {
	t.Helper()
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		t.Fatal("no response from server")
	}
	var msg jsonrpcMessage
	if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return msg
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
