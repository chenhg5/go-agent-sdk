package acp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// Transport handles bidirectional JSON-RPC 2.0 over newline-delimited streams.
// It supports both serving requests from the client and making reverse calls
// (e.g. session/request_permission) back to the client.
type Transport struct {
	in  *bufio.Scanner
	out io.Writer

	mu     sync.Mutex // protects writes to out
	nextID atomic.Int64

	// pending tracks outgoing requests awaiting responses.
	pendingMu sync.Mutex
	pending   map[string]chan *jsonrpcMessage

	// handler processes incoming requests and notifications from the client.
	handler func(msg *jsonrpcMessage)
}

// NewTransport creates a transport over the given reader (stdin) and writer (stdout).
func NewTransport(r io.Reader, w io.Writer) *Transport {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4*1024*1024), 4*1024*1024)
	return &Transport{
		in:      scanner,
		out:     w,
		pending: make(map[string]chan *jsonrpcMessage),
	}
}

// SetHandler sets the callback for incoming requests/notifications.
func (t *Transport) SetHandler(fn func(msg *jsonrpcMessage)) {
	t.handler = fn
}

// ReadLoop reads messages from stdin and dispatches them.
// Blocks until EOF or read error; returns the error.
func (t *Transport) ReadLoop() error {
	for t.in.Scan() {
		line := t.in.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg jsonrpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			t.sendError(nil, errCodeParse, "parse error: "+err.Error())
			continue
		}

		if msg.isResponse() {
			t.resolveResponse(&msg)
		} else if t.handler != nil {
			t.handler(&msg)
		}
	}
	return t.in.Err()
}

// SendResult sends a successful response for the given request id.
func (t *Transport) SendResult(id json.RawMessage, result any) {
	raw, _ := json.Marshal(result)
	t.send(jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Result:  raw,
	})
}

// SendError sends an error response.
func (t *Transport) SendError(id json.RawMessage, code int, message string) {
	t.sendError(id, code, message)
}

// Notify sends a notification (no id, no response expected).
func (t *Transport) Notify(method string, params any) {
	raw, _ := json.Marshal(params)
	t.send(jsonrpcMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  raw,
	})
}

// Call sends a request to the client and blocks until it responds.
// Returns the raw result or an error.
func (t *Transport) Call(method string, params any) (json.RawMessage, error) {
	id := t.nextID.Add(1)
	idJSON, _ := json.Marshal(id)
	raw, _ := json.Marshal(params)

	ch := make(chan *jsonrpcMessage, 1)
	key := string(idJSON)

	t.pendingMu.Lock()
	t.pending[key] = ch
	t.pendingMu.Unlock()

	t.send(jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      idJSON,
		Method:  method,
		Params:  raw,
	})

	resp := <-ch

	t.pendingMu.Lock()
	delete(t.pending, key)
	t.pendingMu.Unlock()

	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

// resolveResponse delivers a response to the waiting Call goroutine.
func (t *Transport) resolveResponse(msg *jsonrpcMessage) {
	key := string(msg.ID)
	t.pendingMu.Lock()
	ch, ok := t.pending[key]
	t.pendingMu.Unlock()
	if ok {
		ch <- msg
	}
}

func (t *Transport) send(msg jsonrpcMessage) {
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	t.mu.Lock()
	_, _ = t.out.Write(data)
	t.mu.Unlock()
}

func (t *Transport) sendError(id json.RawMessage, code int, message string) {
	t.send(jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonrpcError{Code: code, Message: message},
	})
}
