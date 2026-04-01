package agentsdk

import (
	"context"
	"fmt"
	"sync"
)

// ToolExecutor dispatches tool calls and collects results.
// Swap the implementation to change concurrency behaviour or add middleware.
type ToolExecutor interface {
	Execute(ctx context.Context, calls []ToolCall, registry *ToolRegistry) ([]ToolCallResult, error)
}

// ---------------------------------------------------------------------------
// ParallelExecutor — default, runs tools concurrently in goroutines.
// ---------------------------------------------------------------------------

// ParallelExecutor runs every tool call in its own goroutine and waits for
// all to complete. Individual tool errors are captured as is_error results;
// only context cancellation propagates as a top-level error.
type ParallelExecutor struct{}

var _ ToolExecutor = (*ParallelExecutor)(nil)

func (e *ParallelExecutor) Execute(ctx context.Context, calls []ToolCall, registry *ToolRegistry) ([]ToolCallResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	results := make([]ToolCallResult, len(calls))
	var wg sync.WaitGroup

	for i, call := range calls {
		wg.Add(1)
		go func(idx int, c ToolCall) {
			defer wg.Done()
			results[idx] = executeSingle(ctx, c, registry)
		}(i, call)
	}

	wg.Wait()

	if err := ctx.Err(); err != nil {
		return results, err
	}
	return results, nil
}

func executeSingle(ctx context.Context, call ToolCall, registry *ToolRegistry) ToolCallResult {
	t, ok := registry.Get(call.Name)
	if !ok {
		return ToolCallResult{
			ToolUseID: call.ID,
			Content:   fmt.Sprintf("error: tool %q not found", call.Name),
			IsError:   true,
		}
	}

	if v, ok := t.(ToolValidator); ok {
		if err := v.ValidateInput(call.Input); err != nil {
			return ToolCallResult{
				ToolUseID: call.ID,
				Content:   fmt.Sprintf("validation error: %v", err),
				IsError:   true,
			}
		}
	}

	result, err := t.Execute(ctx, call)
	if err != nil {
		return ToolCallResult{
			ToolUseID: call.ID,
			Content:   fmt.Sprintf("execution error: %v", err),
			IsError:   true,
		}
	}

	return ToolCallResult{
		ToolUseID: call.ID,
		Content:   result.Content,
		IsError:   result.IsError,
	}
}

// ---------------------------------------------------------------------------
// SequentialExecutor — runs tools one at a time.
// ---------------------------------------------------------------------------

// SequentialExecutor runs tool calls sequentially. Useful when tools have
// side-effects that must not overlap (e.g. file writes).
type SequentialExecutor struct{}

var _ ToolExecutor = (*SequentialExecutor)(nil)

func (e *SequentialExecutor) Execute(ctx context.Context, calls []ToolCall, registry *ToolRegistry) ([]ToolCallResult, error) {
	results := make([]ToolCallResult, 0, len(calls))
	for _, call := range calls {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		results = append(results, executeSingle(ctx, call, registry))
	}
	return results, nil
}
