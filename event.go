package agentsdk

// EventType classifies user-facing events emitted during an agent run.
type EventType string

const (
	EventTextDelta         EventType = "text_delta"
	EventThinkingDelta     EventType = "thinking_delta"
	EventToolUseStart      EventType = "tool_use_start"
	EventToolResult        EventType = "tool_result"
	EventPermissionRequest EventType = "permission_request"
	EventPermissionResult  EventType = "permission_result"
	EventTurnStart         EventType = "turn_start"
	EventTurnEnd           EventType = "turn_end"
	EventDone              EventType = "done"
	EventError             EventType = "error"
)

// Event is a high-level notification delivered to the caller's EventHandler.
// Exactly one payload field is populated, determined by Type.
type Event struct {
	Type EventType

	// EventTextDelta
	Text string

	// EventThinkingDelta
	Thinking string

	// EventToolUseStart
	ToolUse *EventToolUse

	// EventToolResult
	ToolResultData *EventToolResultData

	// EventPermissionRequest / EventPermissionResult
	Permission *EventPermission

	// EventTurnStart / EventTurnEnd
	Turn int

	// EventDone
	RunResult *RunResult

	// EventError
	Error error
}

// EventToolUse describes the start of a tool invocation.
type EventToolUse struct {
	ID   string
	Name string
}

// EventToolResultData describes the outcome of a single tool call.
type EventToolResultData struct {
	ToolUseID string
	Content   string
	IsError   bool
}

// EventPermission reports permission check activity.
type EventPermission struct {
	ToolName string
	ToolID   string
	Decision PermissionDecision
	Reason   string
}

// EventHandler is a callback invoked for each streaming event.
type EventHandler func(Event)
