package acp

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestTransport_RequestResponse(t *testing.T) {
	// Simulate client sending an initialize request via stdin.
	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}` + "\n"
	in := strings.NewReader(req)
	var out bytes.Buffer

	tp := NewTransport(in, &out)
	var received *jsonrpcMessage
	tp.SetHandler(func(msg *jsonrpcMessage) {
		received = msg
		tp.SendResult(msg.ID, map[string]int{"protocolVersion": 1})
	})

	_ = tp.ReadLoop()

	if received == nil {
		t.Fatal("handler was not called")
	}
	if received.Method != "initialize" {
		t.Errorf("got method %q, want %q", received.Method, "initialize")
	}
	if !received.isRequest() {
		t.Error("expected request, got notification")
	}

	// Verify the response was written.
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 output line, got %d", len(lines))
	}
	var resp jsonrpcMessage
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if string(resp.ID) != "1" {
		t.Errorf("response id = %s, want 1", resp.ID)
	}
}

func TestTransport_Notification(t *testing.T) {
	req := `{"jsonrpc":"2.0","method":"session/cancel","params":{"sessionId":"s1"}}` + "\n"
	in := strings.NewReader(req)
	var out bytes.Buffer

	tp := NewTransport(in, &out)
	var received *jsonrpcMessage
	tp.SetHandler(func(msg *jsonrpcMessage) {
		received = msg
	})

	_ = tp.ReadLoop()

	if received == nil {
		t.Fatal("handler was not called")
	}
	if !received.isNotification() {
		t.Error("expected notification")
	}
	if received.Method != "session/cancel" {
		t.Errorf("got method %q", received.Method)
	}
}

func TestTransport_SendNotify(t *testing.T) {
	in := strings.NewReader("")
	var out bytes.Buffer

	tp := NewTransport(in, &out)
	tp.Notify("session/update", map[string]string{"sessionId": "s1"})

	var msg jsonrpcMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Method != "session/update" {
		t.Errorf("method = %q", msg.Method)
	}
	if msg.ID != nil {
		t.Error("notification should not have id")
	}
}

func TestTransport_SendError(t *testing.T) {
	in := strings.NewReader("")
	var out bytes.Buffer

	tp := NewTransport(in, &out)
	id, _ := json.Marshal(42)
	tp.SendError(id, errCodeMethodNotFound, "not found")

	var msg jsonrpcMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Error == nil {
		t.Fatal("expected error")
	}
	if msg.Error.Code != errCodeMethodNotFound {
		t.Errorf("code = %d", msg.Error.Code)
	}
}

func TestTransport_Call_RoundTrip(t *testing.T) {
	// Use io.Pipe so the response is written only after Call registers its pending entry.
	inReader, inWriter := io.Pipe()
	var out bytes.Buffer

	tp := NewTransport(inReader, &out)

	done := make(chan struct{})
	var callResult json.RawMessage
	var callErr error

	// Start ReadLoop in background.
	go func() { _ = tp.ReadLoop() }()

	// Start Call in background — it sends request, registers pending, then blocks.
	go func() {
		callResult, callErr = tp.Call("session/request_permission", map[string]string{"sessionId": "s1"})
		close(done)
	}()

	// Wait briefly so Call registers the pending channel, then write the matching response.
	// The ID is 1 because it's the first call (nextID starts at 0, Add(1) = 1).
	time.Sleep(50 * time.Millisecond)
	_, _ = inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"outcome":{"outcome":"selected","optionId":"allow-once"}}}` + "\n"))
	_ = inWriter.Close()

	<-done

	if callErr != nil {
		t.Fatalf("Call error: %v", callErr)
	}
	var result RequestPermissionResult
	if err := json.Unmarshal(callResult, &result); err != nil {
		t.Fatal(err)
	}
	if result.Outcome.OptionID != "allow-once" {
		t.Errorf("optionId = %q", result.Outcome.OptionID)
	}
}
