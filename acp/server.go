package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	agentsdk "github.com/chenhg5/go-agent-sdk"
)

// AgentFactory creates a new Agent for a session.
//
// Parameters:
//   - ctx: background context
//   - params: session creation parameters (CWD, MCP servers, etc.)
//   - perm: a pre-built PermissionHandler that delegates to the ACP client
//     via session/request_permission. The factory SHOULD pass it to
//     agentsdk.WithPermissionHandler so that tool-permission requests are
//     forwarded to the connected editor.
type AgentFactory func(ctx context.Context, params NewSessionParams, perm agentsdk.PermissionHandler) (agentsdk.Agent, error)

// ServerConfig configures the ACP server.
type ServerConfig struct {
	// AgentFactory creates an Agent for each new session.
	AgentFactory AgentFactory

	// Info identifies this agent implementation to clients.
	Info *ImplementationInfo

	// Capabilities advertised to the client.
	Capabilities *AgentCapabilities

	// Logger for debug output (writes to stderr). Nil disables logging.
	Logger *log.Logger
}

// Server implements the ACP protocol, bridging JSON-RPC messages
// to go-agent-sdk Agent instances.
type Server struct {
	cfg      ServerConfig
	tp       *Transport
	sessions *SessionManager
	client   *ClientCapabilities
	logger   *log.Logger
}

// NewServer creates an ACP server with the given configuration.
func NewServer(cfg ServerConfig) *Server {
	if cfg.Logger == nil {
		cfg.Logger = log.New(io.Discard, "", 0)
	}
	if cfg.Info == nil {
		cfg.Info = &ImplementationInfo{
			Name:    "go-agent-sdk",
			Title:   "Go Agent SDK",
			Version: "0.6.0",
		}
	}
	if cfg.Capabilities == nil {
		cfg.Capabilities = &AgentCapabilities{
			PromptCapabilities: &PromptCapabilities{EmbeddedContext: true},
		}
	}
	return &Server{
		cfg:      cfg,
		sessions: NewSessionManager(),
		logger:   cfg.Logger,
	}
}

// Run starts the server on stdin/stdout, blocking until EOF.
func (s *Server) Run() error {
	return s.RunOn(os.Stdin, os.Stdout)
}

// RunOn starts the server on custom streams, blocking until EOF.
func (s *Server) RunOn(in io.Reader, out io.Writer) error {
	s.tp = NewTransport(in, out)
	s.tp.SetHandler(s.dispatch)
	return s.tp.ReadLoop()
}

// dispatch routes incoming JSON-RPC messages to the appropriate handler.
func (s *Server) dispatch(msg *jsonrpcMessage) {
	switch msg.Method {
	case "initialize":
		s.handleInitialize(msg)
	case "session/new":
		s.handleNewSession(msg)
	case "session/prompt":
		go s.handlePrompt(msg) // long-running; run in goroutine
	case "session/cancel":
		s.handleCancel(msg)
	default:
		if msg.isRequest() {
			s.tp.SendError(msg.ID, errCodeMethodNotFound, "method not found: "+msg.Method)
		}
	}
}

// ---------------------------------------------------------------------------
// initialize
// ---------------------------------------------------------------------------

func (s *Server) handleInitialize(msg *jsonrpcMessage) {
	var params InitializeParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		s.tp.SendError(msg.ID, errCodeInvalidParams, err.Error())
		return
	}

	s.client = &params.ClientCapabilities
	s.logger.Printf("initialize: client=%v proto=%d", params.ClientInfo, params.ProtocolVersion)

	version := ProtocolVersion
	if params.ProtocolVersion < version {
		version = params.ProtocolVersion
	}

	s.tp.SendResult(msg.ID, InitializeResult{
		ProtocolVersion:   version,
		AgentCapabilities: *s.cfg.Capabilities,
		AgentInfo:         s.cfg.Info,
		AuthMethods:       []AuthMethod{},
	})
}

// ---------------------------------------------------------------------------
// session/new
// ---------------------------------------------------------------------------

func (s *Server) handleNewSession(msg *jsonrpcMessage) {
	var params NewSessionParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		s.tp.SendError(msg.ID, errCodeInvalidParams, err.Error())
		return
	}

	sessionID := generateID()
	permHandler := s.MakePermissionHandler(sessionID)

	agent, err := s.cfg.AgentFactory(context.Background(), params, permHandler)
	if err != nil {
		s.tp.SendError(msg.ID, errCodeInternal, "failed to create agent: "+err.Error())
		return
	}

	sess := s.sessions.CreateWithID(sessionID, params.CWD, agent)
	s.logger.Printf("session/new: id=%s cwd=%s", sess.ID, params.CWD)

	s.tp.SendResult(msg.ID, NewSessionResult{SessionID: sess.ID})
}

// ---------------------------------------------------------------------------
// session/prompt
// ---------------------------------------------------------------------------

func (s *Server) handlePrompt(msg *jsonrpcMessage) {
	var params PromptParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		s.tp.SendError(msg.ID, errCodeInvalidParams, err.Error())
		return
	}

	sess, ok := s.sessions.Get(params.SessionID)
	if !ok {
		s.tp.SendError(msg.ID, errCodeInvalidParams, "unknown session: "+params.SessionID)
		return
	}

	userText := extractText(params.Prompt)
	s.logger.Printf("session/prompt: session=%s text=%q", params.SessionID, truncate(userText, 80))

	ctx, cancel := context.WithCancel(context.Background())
	sess.SetCancel(cancel)
	defer func() {
		sess.SetCancel(nil)
		cancel()
	}()

	handler := s.makeEventHandler(params.SessionID)
	result, err := sess.Agent.RunStream(ctx, userText, handler)
	if err != nil {
		if ctx.Err() != nil {
			s.tp.SendResult(msg.ID, PromptResult{StopReason: StopReasonCancelled})
			return
		}
		s.tp.SendError(msg.ID, errCodeInternal, err.Error())
		return
	}

	stopReason := mapStopReason(result.Reason)
	s.tp.SendResult(msg.ID, PromptResult{StopReason: stopReason})
}

// makeEventHandler returns an agentsdk.EventHandler that translates SDK events
// into ACP session/update notifications streamed to the client.
func (s *Server) makeEventHandler(sessionID string) agentsdk.EventHandler {
	return func(ev agentsdk.Event) {
		switch ev.Type {
		case agentsdk.EventTextDelta:
			s.sendUpdate(sessionID, MessageChunkUpdate{
				SessionUpdate: "agent_message_chunk",
				Content: ContentBlock{
					Type: "text",
					Text: ev.Text,
				},
			})

		case agentsdk.EventThinkingDelta:
			s.sendUpdate(sessionID, MessageChunkUpdate{
				SessionUpdate: "thought_message_chunk",
				Content: ContentBlock{
					Type: "text",
					Text: ev.Thinking,
				},
			})

		case agentsdk.EventToolUseStart:
			if ev.ToolUse == nil {
				return
			}
			s.sendUpdate(sessionID, ToolCallNotification{
				SessionUpdate: "tool_call",
				ToolCallID:    ev.ToolUse.ID,
				Title:         ev.ToolUse.Name,
				Kind:          classifyToolKind(ev.ToolUse.Name),
				Status:        "pending",
			})

		case agentsdk.EventToolUseInput:
			if ev.ToolUse == nil {
				return
			}
			s.sendUpdate(sessionID, ToolCallNotification{
				SessionUpdate: "tool_call",
				ToolCallID:    ev.ToolUse.ID,
				Title:         buildToolTitle(ev.ToolUse.Name, ev.ToolUse.Input),
				Kind:          classifyToolKind(ev.ToolUse.Name),
				Status:        "in_progress",
				RawInput:      ev.ToolUse.Input,
			})

		case agentsdk.EventToolResult:
			if ev.ToolResultData == nil {
				return
			}
			status := "completed"
			if ev.ToolResultData.IsError {
				status = "error"
			}
			s.sendUpdate(sessionID, ToolCallStatusUpdate{
				SessionUpdate: "tool_call_update",
				ToolCallID:    ev.ToolResultData.ToolUseID,
				Status:        status,
				Content: []ToolCallContent{{
					Type: "content",
					Content: &ContentBlock{
						Type: "text",
						Text: ev.ToolResultData.Content,
					},
				}},
			})

		case agentsdk.EventPermissionRequest:
			if ev.Permission != nil {
				s.logger.Printf("permission_request: tool=%s", ev.Permission.ToolName)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// session/cancel
// ---------------------------------------------------------------------------

func (s *Server) handleCancel(msg *jsonrpcMessage) {
	var params CancelParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return
	}
	if sess, ok := s.sessions.Get(params.SessionID); ok {
		sess.Cancel()
		s.logger.Printf("session/cancel: session=%s", params.SessionID)
	}
}

// ---------------------------------------------------------------------------
// Reverse calls to client
// ---------------------------------------------------------------------------

// RequestPermission sends a session/request_permission call to the client
// and blocks until the user responds. Returns the selected option ID.
func (s *Server) RequestPermission(sessionID, toolCallID, toolName string) (string, error) {
	params := RequestPermissionParams{
		SessionID: sessionID,
		ToolCall: ToolCallUpdateData{
			ToolCallID: toolCallID,
			Title:      toolName,
			Status:     "pending",
		},
		Options: []PermissionOption{
			{OptionID: "allow-once", Name: "Allow once", Kind: "allow_once"},
			{OptionID: "allow-always", Name: "Allow always", Kind: "allow_always"},
			{OptionID: "reject-once", Name: "Reject", Kind: "reject_once"},
		},
	}

	raw, err := s.tp.Call("session/request_permission", params)
	if err != nil {
		return "", err
	}

	var result RequestPermissionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("unmarshal permission result: %w", err)
	}
	if result.Outcome.Outcome == "cancelled" {
		return "cancelled", nil
	}
	return result.Outcome.OptionID, nil
}

// MakePermissionHandler returns an agentsdk.PermissionHandler that asks the
// ACP client for permission using session/request_permission.
func (s *Server) MakePermissionHandler(sessionID string) agentsdk.PermissionHandler {
	return func(ctx context.Context, req agentsdk.PermissionRequest) agentsdk.PermissionResponse {
		optionID, err := s.RequestPermission(sessionID, req.Call.ID, req.Call.Name)
		if err != nil {
			s.logger.Printf("permission error: %v", err)
			return agentsdk.PermissionResponse{
				Decision: agentsdk.PermissionDeny,
				Reason:   "permission request failed: " + err.Error(),
			}
		}

		switch optionID {
		case "allow-once", "allow-always":
			return agentsdk.PermissionResponse{Decision: agentsdk.PermissionAllow}
		case "cancelled":
			return agentsdk.PermissionResponse{
				Decision: agentsdk.PermissionDeny,
				Reason:   "user cancelled the operation",
			}
		default:
			return agentsdk.PermissionResponse{
				Decision: agentsdk.PermissionDeny,
				Reason:   "user rejected: " + optionID,
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *Server) sendUpdate(sessionID string, update any) {
	s.tp.Notify("session/update", SessionUpdateParams{
		SessionID: sessionID,
		Update:    update,
	})
}

func mapStopReason(r agentsdk.TerminalReason) StopReason {
	switch r {
	case agentsdk.ReasonEndTurn:
		return StopReasonEndTurn
	case agentsdk.ReasonMaxTurns:
		return StopReasonMaxTurns
	case agentsdk.ReasonAborted:
		return StopReasonCancelled
	default:
		return StopReasonEndTurn
	}
}

func classifyToolKind(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "read") || strings.Contains(lower, "glob") || strings.Contains(lower, "grep"):
		return "read"
	case strings.Contains(lower, "edit") || strings.Contains(lower, "write"):
		return "edit"
	case strings.Contains(lower, "bash") || strings.Contains(lower, "exec"):
		return "execute"
	case strings.Contains(lower, "search"):
		return "search"
	case strings.Contains(lower, "think"):
		return "think"
	default:
		return "other"
	}
}

// buildToolTitle generates a human-readable title from tool name and input JSON.
// e.g. "file_write → /src/main.go", "bash → ls -la", "grep → pattern in /src"
func buildToolTitle(name string, input json.RawMessage) string {
	if len(input) == 0 {
		return name
	}

	var m map[string]json.RawMessage
	if json.Unmarshal(input, &m) != nil {
		return name
	}

	str := func(key string) string {
		raw, ok := m[key]
		if !ok || len(raw) < 2 {
			return ""
		}
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return ""
		}
		return s
	}

	trim := func(s string, max int) string {
		if len(s) <= max {
			return s
		}
		return s[:max] + "…"
	}

	switch strings.ToLower(name) {
	case "file_write", "filewrite", "write":
		if p := str("file_path"); p != "" {
			return name + " → " + p
		}
		if p := str("path"); p != "" {
			return name + " → " + p
		}
	case "file_read", "fileread", "read":
		if p := str("file_path"); p != "" {
			return name + " → " + p
		}
		if p := str("path"); p != "" {
			return name + " → " + p
		}
	case "file_edit", "fileedit", "edit":
		if p := str("file_path"); p != "" {
			return name + " → " + p
		}
		if p := str("path"); p != "" {
			return name + " → " + p
		}
	case "bash", "shell", "exec":
		if c := str("command"); c != "" {
			return name + " → " + trim(c, 80)
		}
	case "glob":
		if p := str("pattern"); p != "" {
			return name + " → " + p
		}
	case "grep":
		if p := str("pattern"); p != "" {
			title := name + " → " + trim(p, 40)
			if d := str("path"); d != "" {
				title += " in " + d
			}
			return title
		}
	}

	// Fallback: try common parameter names for unknown tools
	for _, key := range []string{"file_path", "path", "command", "url", "query", "name"} {
		if v := str(key); v != "" {
			return name + " → " + trim(v, 80)
		}
	}

	return name
}

func extractText(blocks []ContentBlock) string {
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "resource":
			if b.Resource != nil && b.Resource.Text != "" {
				parts = append(parts, fmt.Sprintf("\n<file uri=%q>\n%s\n</file>", b.Resource.URI, b.Resource.Text))
			}
		}
	}
	return strings.Join(parts, "\n")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
