package agentsdk

import (
	"context"
	"encoding/json"
	"fmt"
)

// PermissionHandler is called before every tool execution.
// Return PermissionAllow to proceed, PermissionDeny to block.
//
// The handler can block (e.g., waiting for user input or a channel signal);
// the agent loop pauses naturally until the handler returns. Use the ctx
// for cancellation and timeouts.
type PermissionHandler func(ctx context.Context, req PermissionRequest) PermissionResponse

// PermissionRequest carries the information needed to make a permission decision.
type PermissionRequest struct {
	Tool ToolSpec // the tool definition
	Call ToolCall // the pending invocation (ID, name, raw input)
}

// PermissionResponse carries the handler's decision.
type PermissionResponse struct {
	Decision PermissionDecision
	Reason   string // human-readable explanation (used in deny/error feedback to LLM)

	// ModifiedInput optionally replaces the tool call's Input before execution.
	// This allows the permission handler to sanitise or transform arguments.
	// Ignored when Decision is Deny.
	ModifiedInput json.RawMessage
}

// ---------------------------------------------------------------------------
// Built-in policies
// ---------------------------------------------------------------------------

// AllowAll is a PermissionHandler that allows every tool call unconditionally.
func AllowAll(_ context.Context, _ PermissionRequest) PermissionResponse {
	return PermissionResponse{Decision: PermissionAllow}
}

// DenyAll is a PermissionHandler that blocks every tool call.
func DenyAll(_ context.Context, _ PermissionRequest) PermissionResponse {
	return PermissionResponse{
		Decision: PermissionDeny,
		Reason:   "all tool calls are blocked by policy",
	}
}

// ReadOnlyPermission allows only tools whose ToolPermChecker reports read-only,
// and denies anything else. If a tool does not implement ToolPermChecker it is
// denied by default.
func ReadOnlyPermission(registry *ToolRegistry) PermissionHandler {
	return func(_ context.Context, req PermissionRequest) PermissionResponse {
		t, ok := registry.Get(req.Call.Name)
		if !ok {
			return PermissionResponse{Decision: PermissionDeny, Reason: "unknown tool"}
		}
		if checker, ok := t.(ToolPermChecker); ok {
			decision, _ := checker.CheckPermission(req.Call)
			if decision == PermissionAllow {
				return PermissionResponse{Decision: PermissionAllow}
			}
		}
		return PermissionResponse{Decision: PermissionDeny, Reason: "write operations are not permitted in read-only mode"}
	}
}

// ---------------------------------------------------------------------------
// Interactive helpers
// ---------------------------------------------------------------------------

// InteractivePermissionFunc is called when a tool-level check returns
// PermissionAsk. The function should block until the user responds.
// Return Allow or Deny.
type InteractivePermissionFunc func(ctx context.Context, req PermissionRequest) PermissionResponse

// WithToolCheckerAndPrompter creates a two-phase permission handler:
//
//  1. Each tool's ToolPermChecker is consulted (if implemented).
//     Allow → execute immediately. Deny → block immediately.
//  2. If the checker returns Ask (or the tool has no checker), the prompter
//     is called. The prompter can block waiting for user confirmation.
//
// If prompter is nil, Ask falls through to Allow (for non-interactive use).
func WithToolCheckerAndPrompter(registry *ToolRegistry, prompter InteractivePermissionFunc) PermissionHandler {
	return func(ctx context.Context, req PermissionRequest) PermissionResponse {
		t, ok := registry.Get(req.Call.Name)
		if !ok {
			if prompter != nil {
				return prompter(ctx, req)
			}
			return PermissionResponse{Decision: PermissionAllow}
		}

		checker, hasChecker := t.(ToolPermChecker)
		if !hasChecker {
			if prompter != nil {
				return prompter(ctx, req)
			}
			return PermissionResponse{Decision: PermissionAllow}
		}

		decision, _ := checker.CheckPermission(req.Call)
		switch decision {
		case PermissionAllow:
			return PermissionResponse{Decision: PermissionAllow}
		case PermissionDeny:
			return PermissionResponse{Decision: PermissionDeny, Reason: "denied by tool policy"}
		default: // Ask
			if prompter != nil {
				return prompter(ctx, req)
			}
			return PermissionResponse{Decision: PermissionAllow}
		}
	}
}

// ChannelPermission creates a permission handler that sends each request to
// a channel and waits for the response. Useful for event-driven UIs (web apps,
// TUI frameworks) where permission decisions arrive asynchronously.
//
//	requests, responses := make(chan PermissionRequest, 1), make(chan PermissionResponse, 1)
//	agent, _ := agentsdk.New(agentsdk.WithPermissionHandler(
//	    agentsdk.ChannelPermission(requests, responses),
//	))
//	// In your UI goroutine:
//	req := <-requests
//	responses <- PermissionResponse{Decision: PermissionAllow}
func ChannelPermission(requests chan<- PermissionRequest, responses <-chan PermissionResponse) PermissionHandler {
	return func(ctx context.Context, req PermissionRequest) PermissionResponse {
		select {
		case requests <- req:
		case <-ctx.Done():
			return PermissionResponse{Decision: PermissionDeny, Reason: fmt.Sprintf("context cancelled: %v", ctx.Err())}
		}
		select {
		case resp := <-responses:
			return resp
		case <-ctx.Done():
			return PermissionResponse{Decision: PermissionDeny, Reason: fmt.Sprintf("context cancelled: %v", ctx.Err())}
		}
	}
}
