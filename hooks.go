package agentsdk

import "context"

// Hooks contains optional callbacks invoked at key points during the agent loop.
// Every field is independently optional — nil fields are silently skipped.
type Hooks struct {
	// BeforeToolCall is invoked before a single tool is executed.
	// Return a non-nil error to skip execution and feed the error
	// back to the LLM as an is_error tool_result.
	BeforeToolCall func(ctx context.Context, call ToolCall) error

	// AfterToolCall is invoked after a single tool finishes (success or error).
	AfterToolCall func(ctx context.Context, call ToolCall, result ToolCallResult)

	// BeforeTurn is invoked at the start of each agentic turn, before
	// calling the LLM. Useful for logging, injecting context, etc.
	BeforeTurn func(ctx context.Context, turn int, messages []Message)

	// AfterTurn is invoked after the LLM responds and tools are executed.
	AfterTurn func(ctx context.Context, turn int, usage Usage)

	// OnError is invoked when the loop encounters a non-fatal error
	// (e.g. a retryable provider failure). Return true to continue,
	// false to abort.
	OnError func(ctx context.Context, err error) bool
}

// fireBeforeToolCall safely calls BeforeToolCall if set.
func (h *Hooks) fireBeforeToolCall(ctx context.Context, call ToolCall) error {
	if h == nil || h.BeforeToolCall == nil {
		return nil
	}
	return h.BeforeToolCall(ctx, call)
}

// fireAfterToolCall safely calls AfterToolCall if set.
func (h *Hooks) fireAfterToolCall(ctx context.Context, call ToolCall, result ToolCallResult) {
	if h == nil || h.AfterToolCall == nil {
		return
	}
	h.AfterToolCall(ctx, call, result)
}

// fireBeforeTurn safely calls BeforeTurn if set.
func (h *Hooks) fireBeforeTurn(ctx context.Context, turn int, messages []Message) {
	if h == nil || h.BeforeTurn == nil {
		return
	}
	h.BeforeTurn(ctx, turn, messages)
}

// fireAfterTurn safely calls AfterTurn if set.
func (h *Hooks) fireAfterTurn(ctx context.Context, turn int, usage Usage) {
	if h == nil || h.AfterTurn == nil {
		return
	}
	h.AfterTurn(ctx, turn, usage)
}
