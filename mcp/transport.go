package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// Transport is a low-level JSON-RPC transport layer.
type Transport interface {
	// Send sends a request and returns the response. Thread-safe.
	Send(ctx context.Context, req *jsonrpcRequest) (*jsonrpcResponse, error)
	// Notify sends a one-way notification (no response expected).
	Notify(ctx context.Context, method string, params any) error
	// Close shuts down the transport.
	Close() error
}

// ---------------------------------------------------------------------------
// Stdio Transport — runs an MCP server as a subprocess
// ---------------------------------------------------------------------------

// StdioTransport communicates with an MCP server over stdin/stdout of a child
// process. Each JSON-RPC message is a single line (newline-delimited JSON).
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr io.ReadCloser

	mu       sync.Mutex
	nextID   atomic.Int64
	pending  map[int64]chan *jsonrpcResponse
	closed   bool
	closeErr error
	done     chan struct{}
}

// NewStdioTransport starts the given command and returns a transport connected
// to its stdin/stdout. The command is started immediately.
func NewStdioTransport(ctx context.Context, name string, args ...string) (*StdioTransport, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp: start %q: %w", name, err)
	}

	t := &StdioTransport{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReaderSize(stdout, 256*1024),
		stderr:  stderr,
		pending: make(map[int64]chan *jsonrpcResponse),
		done:    make(chan struct{}),
	}

	go t.readLoop()
	return t, nil
}

func (t *StdioTransport) Send(ctx context.Context, req *jsonrpcRequest) (*jsonrpcResponse, error) {
	if req.ID == 0 {
		req.ID = t.nextID.Add(1)
	}

	ch := make(chan *jsonrpcResponse, 1)
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, fmt.Errorf("mcp: transport closed")
	}
	t.pending[req.ID] = ch
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		delete(t.pending, req.ID)
		t.mu.Unlock()
	}()

	if err := t.writeJSON(req); err != nil {
		return nil, err
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.done:
		return nil, fmt.Errorf("mcp: transport closed while waiting for response")
	}
}

func (t *StdioTransport) Notify(_ context.Context, method string, params any) error {
	req := &jsonrpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return t.writeJSON(req)
}

func (t *StdioTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return t.closeErr
	}
	t.closed = true
	t.mu.Unlock()

	t.stdin.Close()
	t.closeErr = t.cmd.Wait()
	close(t.done)
	return t.closeErr
}

func (t *StdioTransport) writeJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("mcp: marshal: %w", err)
	}
	data = append(data, '\n')
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err = t.stdin.Write(data)
	return err
}

func (t *StdioTransport) readLoop() {
	for {
		line, err := t.stdout.ReadBytes('\n')
		if err != nil {
			return
		}

		var resp jsonrpcResponse
		if json.Unmarshal(line, &resp) != nil {
			continue
		}

		// Notifications from server (id == 0) are ignored for now.
		if resp.ID == 0 {
			continue
		}

		t.mu.Lock()
		ch, ok := t.pending[resp.ID]
		t.mu.Unlock()
		if ok {
			ch <- &resp
		}
	}
}
