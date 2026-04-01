package agentsdk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// loopState is the mutable state carried across iterations of the agent loop.
type loopState struct {
	messages     []Message
	turnCount    int
	usage        Usage
	systemPrompt string        // resolved once per Run
	systemBlocks []SystemBlock // structured alternative (takes precedence)
}

// resolvePrompt builds the system prompt once per Run.
// Priority: PromptBuilder > SystemPrompt string, with AppendPrompt appended.
func (a *agent) resolvePrompt(ctx context.Context, state *loopState) error {
	if a.config.PromptBuilder != nil {
		blocks, err := a.config.PromptBuilder.BuildBlocks(ctx)
		if err != nil {
			return fmt.Errorf("prompt builder: %w", err)
		}
		state.systemBlocks = blocks
		return nil
	}

	prompt := a.config.SystemPrompt
	if a.config.AppendPrompt != "" {
		if prompt != "" {
			prompt += "\n\n"
		}
		prompt += a.config.AppendPrompt
	}
	state.systemPrompt = prompt
	return nil
}

// resolveContext gathers dynamic context from ContextProviders and injects
// it into the first user message wrapped in <system-reminder> tags.
func (a *agent) resolveContext(ctx context.Context, state *loopState) error {
	if len(a.config.ContextProviders) == 0 {
		return nil
	}

	parts := make(map[string]string)
	for _, p := range a.config.ContextProviders {
		text, err := p.Provide(ctx)
		if err != nil {
			return fmt.Errorf("context provider %q: %w", p.Name(), err)
		}
		if text != "" {
			parts[p.Name()] = text
		}
	}

	if len(parts) > 0 {
		state.messages = InjectContext(state.messages, WrapUserContext(parts))
	}
	return nil
}

// runLoop is the core agentic loop:
//
//	resolve prompt → resolve context → compact → build params →
//	stream LLM → permission check → hook:before → execute tools →
//	hook:after → track cost → repeat
func (a *agent) runLoop(ctx context.Context, state *loopState, handler EventHandler) (*RunResult, error) {
	// Resolve system prompt and user context once before the loop starts.
	if err := a.resolvePrompt(ctx, state); err != nil {
		return nil, err
	}
	if err := a.resolveContext(ctx, state); err != nil {
		return nil, err
	}

	for {
		state.turnCount++

		// --- guard: max turns ---
		if a.config.MaxTurns > 0 && state.turnCount > a.config.MaxTurns {
			return a.buildResult(state, ReasonMaxTurns), nil
		}

		// --- hook: before turn ---
		a.config.Hooks.fireBeforeTurn(ctx, state.turnCount, state.messages)
		emit(handler, Event{Type: EventTurnStart, Turn: state.turnCount})

		// --- auto-compact if needed ---
		if needsCompact(a.config.Compact, state.messages) {
			state.messages = applyCompact(a.config.Compact, state.messages)
		}

		// --- build request ---
		params := a.buildParams(state)

		// --- stream LLM response ---
		stream, err := a.config.Provider.CreateMessageStream(ctx, params)
		if err != nil {
			return nil, err
		}

		assistantMsg, toolCalls, turnUsage, err := consumeStream(ctx, stream, handler)
		_ = stream.Close()
		if err != nil {
			return nil, err
		}

		state.usage = state.usage.Add(turnUsage)
		state.messages = append(state.messages, assistantMsg)

		// --- track cost ---
		if a.config.CostTracker != nil {
			a.config.CostTracker.AddUsage(a.config.Model, turnUsage)
		}

		// --- hook: after turn ---
		a.config.Hooks.fireAfterTurn(ctx, state.turnCount, turnUsage)
		emit(handler, Event{Type: EventTurnEnd, Turn: state.turnCount})

		// --- no tool calls → conversation complete ---
		if len(toolCalls) == 0 {
			return a.buildResult(state, ReasonEndTurn), nil
		}

		// --- permission check + tool execution ---
		results, err := a.executeWithPermissions(ctx, toolCalls, handler)
		if err != nil {
			return nil, err
		}

		// --- append tool results as a user message ---
		resultBlocks := make([]ContentBlock, len(results))
		for i, r := range results {
			resultBlocks[i] = NewToolResultBlock(r.ToolUseID, r.Content, r.IsError)
		}
		state.messages = append(state.messages, NewToolResultMessage(resultBlocks...))
	}
}

// executeWithPermissions checks permissions, fires hooks, runs tools, and
// emits events — all while preserving the original call order.
func (a *agent) executeWithPermissions(ctx context.Context, calls []ToolCall, handler EventHandler) ([]ToolCallResult, error) {
	results := make([]ToolCallResult, len(calls))
	var allowed []ToolCall
	var allowedIdx []int

	// --- 1. permission gate ---
	for i, call := range calls {
		emit(handler, Event{
			Type:       EventPermissionRequest,
			Permission: &EventPermission{ToolName: call.Name, ToolID: call.ID},
		})

		decision := a.checkPermission(ctx, call)

		switch decision.Decision {
		case PermissionDeny:
			reason := decision.Reason
			if reason == "" {
				reason = "tool call denied by permission handler"
			}
			emit(handler, Event{
				Type:       EventPermissionResult,
				Permission: &EventPermission{ToolName: call.Name, ToolID: call.ID, Decision: PermissionDeny, Reason: reason},
			})
			results[i] = ToolCallResult{
				ToolUseID: call.ID,
				Content:   fmt.Sprintf("permission denied: %s", reason),
				IsError:   true,
			}
			emit(handler, Event{
				Type: EventToolResult,
				ToolResultData: &EventToolResultData{
					ToolUseID: call.ID, Content: results[i].Content, IsError: true,
				},
			})

		default: // Allow or Ask (Ask without handler falls through to Allow)
			emit(handler, Event{
				Type:       EventPermissionResult,
				Permission: &EventPermission{ToolName: call.Name, ToolID: call.ID, Decision: PermissionAllow},
			})
			if len(decision.ModifiedInput) > 0 {
				call.Input = decision.ModifiedInput
			}
			allowed = append(allowed, call)
			allowedIdx = append(allowedIdx, i)
		}
	}

	if len(allowed) == 0 {
		return results, nil
	}

	// --- 2. hook: before each tool ---
	var toExecute []ToolCall
	var executeIdx []int
	for j, call := range allowed {
		if err := a.config.Hooks.fireBeforeToolCall(ctx, call); err != nil {
			idx := allowedIdx[j]
			results[idx] = ToolCallResult{
				ToolUseID: call.ID,
				Content:   fmt.Sprintf("blocked by hook: %v", err),
				IsError:   true,
			}
			emit(handler, Event{
				Type: EventToolResult,
				ToolResultData: &EventToolResultData{
					ToolUseID: call.ID, Content: results[idx].Content, IsError: true,
				},
			})
		} else {
			toExecute = append(toExecute, call)
			executeIdx = append(executeIdx, allowedIdx[j])
		}
	}

	// --- 3. execute ---
	if len(toExecute) > 0 {
		executed, err := a.config.Executor.Execute(ctx, toExecute, a.config.Tools)
		if err != nil {
			return nil, err
		}
		for j, r := range executed {
			idx := executeIdx[j]
			results[idx] = r

			// hook: after each tool
			a.config.Hooks.fireAfterToolCall(ctx, toExecute[j], r)

			emit(handler, Event{
				Type: EventToolResult,
				ToolResultData: &EventToolResultData{
					ToolUseID: r.ToolUseID, Content: r.Content, IsError: r.IsError,
				},
			})
		}
	}

	return results, nil
}

// checkPermission evaluates the PermissionHandler (if set) for a single call.
func (a *agent) checkPermission(ctx context.Context, call ToolCall) PermissionResponse {
	if a.config.PermissionHandler == nil {
		return PermissionResponse{Decision: PermissionAllow}
	}

	var spec ToolSpec
	if t, ok := a.config.Tools.Get(call.Name); ok {
		spec = t.Definition()
	} else {
		spec = ToolSpec{Name: call.Name}
	}

	return a.config.PermissionHandler(ctx, PermissionRequest{
		Tool: spec,
		Call: call,
	})
}

// buildResult constructs a RunResult and attaches cost if a tracker is active.
func (a *agent) buildResult(state *loopState, reason TerminalReason) *RunResult {
	r := &RunResult{
		Messages: state.messages,
		Reason:   reason,
		Usage:    state.usage,
	}
	if a.config.CostTracker != nil {
		r.Cost = a.config.CostTracker.TotalCost()
	}
	return r
}

// buildParams assembles the MessageParams for a single LLM call.
func (a *agent) buildParams(state *loopState) *MessageParams {
	p := &MessageParams{
		Model:         a.config.Model,
		Messages:      state.messages,
		MaxTokens:     a.config.MaxTokens,
		Temperature:   a.config.Temperature,
		TopP:          a.config.TopP,
		TopK:          a.config.TopK,
		StopSequences: a.config.StopSequences,
		ToolChoice:    a.config.ToolChoice,
		Thinking:      a.config.Thinking,
		Stream:        true,
	}

	if len(state.systemBlocks) > 0 {
		p.SystemBlocks = state.systemBlocks
	} else {
		p.System = state.systemPrompt
	}

	pctx := PromptContext{
		Tools: a.config.Tools.Names(),
		Model: a.config.Model,
	}
	if specs := a.config.Tools.SpecsWithContext(pctx); len(specs) > 0 {
		p.Tools = specs
	}
	return p
}

// consumeStream reads every event from a Stream, accumulates the full
// assistant message and any tool-call requests, and forwards deltas to
// the EventHandler.
func consumeStream(_ context.Context, stream Stream, handler EventHandler) (Message, []ToolCall, Usage, error) {
	var (
		blocks       []ContentBlock
		toolCalls    []ToolCall
		usage        Usage
		currentBlock *ContentBlock
		inputJSON    strings.Builder
	)

	for {
		evt, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return Message{}, nil, Usage{}, err
		}

		switch evt.Type {
		case StreamEventMessageStart:
			if evt.Message != nil {
				usage = evt.Message.Usage
			}

		case StreamEventContentStart:
			if evt.Block == nil {
				continue
			}
			blk := ContentBlock{Type: evt.Block.Type}
			if evt.Block.Type == "tool_use" {
				blk.ID = evt.Block.ID
				blk.Name = evt.Block.Name
				inputJSON.Reset()
				emit(handler, Event{
					Type:    EventToolUseStart,
					ToolUse: &EventToolUse{ID: blk.ID, Name: blk.Name},
				})
			}
			currentBlock = &blk

		case StreamEventContentDelta:
			if evt.Delta == nil || currentBlock == nil {
				continue
			}
			switch evt.Delta.Type {
			case "text_delta":
				currentBlock.Text += evt.Delta.Text
				emit(handler, Event{Type: EventTextDelta, Text: evt.Delta.Text})
			case "thinking_delta":
				currentBlock.Thinking += evt.Delta.Thinking
				emit(handler, Event{Type: EventThinkingDelta, Thinking: evt.Delta.Thinking})
			case "input_json_delta":
				inputJSON.WriteString(evt.Delta.JSON)
			}

		case StreamEventContentStop:
			if currentBlock == nil {
				continue
			}
			if currentBlock.Type == "tool_use" {
				if raw := inputJSON.String(); raw != "" {
					currentBlock.Input = json.RawMessage(raw)
				}
				toolCalls = append(toolCalls, ToolCall{
					ID: currentBlock.ID, Name: currentBlock.Name,
					Input: currentBlock.Input,
				})
				emit(handler, Event{
					Type:    EventToolUseInput,
					ToolUse: &EventToolUse{ID: currentBlock.ID, Name: currentBlock.Name, Input: currentBlock.Input},
				})
			}
			blocks = append(blocks, *currentBlock)
			currentBlock = nil

		case StreamEventMessageDelta:
			if evt.Usage != nil {
				usage.OutputTokens = evt.Usage.OutputTokens
			}

		case StreamEventMessageStop, StreamEventPing:
			// no-op
		}
	}

	return NewAssistantMessage(blocks...), toolCalls, usage, nil
}

func emit(handler EventHandler, evt Event) {
	if handler != nil {
		handler(evt)
	}
}
