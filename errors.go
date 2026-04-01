package agentsdk

import "fmt"

// TerminalReason explains why the agent loop stopped.
type TerminalReason string

const (
	ReasonEndTurn  TerminalReason = "end_turn"
	ReasonMaxTurns TerminalReason = "max_turns"
	ReasonAborted  TerminalReason = "aborted"
	ReasonError    TerminalReason = "error"
)

// RunResult is returned when the agent loop exits.
type RunResult struct {
	Messages []Message
	Reason   TerminalReason
	Usage    Usage
	Cost     float64 // estimated cost in USD (populated when CostTracker is set)
}

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

// ErrAlreadyRunning is returned when Run is called on an agent that is
// already executing a loop.
type ErrAlreadyRunning struct{}

func (ErrAlreadyRunning) Error() string { return "agent is already running" }

// ErrNoProvider is returned when no Provider has been configured.
type ErrNoProvider struct{}

func (ErrNoProvider) Error() string { return "no LLM provider configured; use WithProvider option" }

// ErrMaxTurns is returned when the agent hits the turn limit.
type ErrMaxTurns struct{ Turns int }

func (e ErrMaxTurns) Error() string {
	return fmt.Sprintf("agent reached maximum turns (%d)", e.Turns)
}

// ProviderError wraps an error originating from the LLM provider.
type ProviderError struct {
	StatusCode int
	Type       string
	Message    string
	Cause      error
}

func (e *ProviderError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("provider error (status %d, %s): %s: %v", e.StatusCode, e.Type, e.Message, e.Cause)
	}
	return fmt.Sprintf("provider error (status %d, %s): %s", e.StatusCode, e.Type, e.Message)
}

func (e *ProviderError) Unwrap() error { return e.Cause }
