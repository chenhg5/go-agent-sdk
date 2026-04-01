package agentsdk

import (
	"context"
	"sync"
)

// Agent is the top-level interface for interacting with an LLM agent.
// It manages conversation state, dispatches tool calls, and streams events.
type Agent interface {
	// Run appends the prompt as a user message and runs the agent loop
	// until the model stops or the turn limit is reached.
	Run(ctx context.Context, prompt string) (*RunResult, error)

	// RunStream is like Run but delivers streaming events to handler.
	RunStream(ctx context.Context, prompt string, handler EventHandler) (*RunResult, error)

	// RunMessages runs the loop with an explicit message list instead of
	// appending a simple text prompt. Useful for multi-modal input.
	RunMessages(ctx context.Context, msgs []Message, handler EventHandler) (*RunResult, error)

	// Messages returns the current conversation history (snapshot).
	Messages() []Message

	// SetMessages replaces the conversation history.
	SetMessages(msgs []Message)

	// Reset clears the conversation history.
	Reset()

	// Config returns a copy of the active configuration.
	Config() Config

	// CostTracker returns the cost tracker (may be nil).
	CostTracker() CostTracker
}

// New creates a new Agent with the given options.
func New(opts ...Option) (Agent, error) {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.Provider == nil {
		return nil, ErrNoProvider{}
	}
	a := &agent{config: cfg}

	// Restore conversation from store if configured.
	if cfg.Store != nil && cfg.ConversationID != "" {
		if msgs, err := cfg.Store.Load(cfg.ConversationID); err == nil && len(msgs) > 0 {
			a.messages = msgs
		}
	}

	return a, nil
}

// ---------------------------------------------------------------------------
// Implementation
// ---------------------------------------------------------------------------

type agent struct {
	config   Config
	messages []Message

	mu      sync.Mutex
	running bool
}

func (a *agent) Run(ctx context.Context, prompt string) (*RunResult, error) {
	return a.RunStream(ctx, prompt, nil)
}

func (a *agent) RunStream(ctx context.Context, prompt string, handler EventHandler) (*RunResult, error) {
	return a.RunMessages(ctx, []Message{NewUserMessage(prompt)}, handler)
}

func (a *agent) RunMessages(ctx context.Context, msgs []Message, handler EventHandler) (*RunResult, error) {
	if err := a.acquireLock(); err != nil {
		return nil, err
	}
	defer a.releaseLock()

	a.messages = append(a.messages, msgs...)

	state := &loopState{
		messages: a.messages,
	}

	result, err := a.runLoop(ctx, state, handler)
	if err != nil {
		// Even on error, persist what we have.
		a.messages = state.messages
		a.persist()
		return nil, err
	}

	a.messages = state.messages
	a.persist()
	return result, nil
}

func (a *agent) Messages() []Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Message, len(a.messages))
	copy(out, a.messages)
	return out
}

func (a *agent) SetMessages(msgs []Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.messages = make([]Message, len(msgs))
	copy(a.messages, msgs)
	a.persist()
}

func (a *agent) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.messages = nil
	if a.config.CostTracker != nil {
		a.config.CostTracker.Reset()
	}
}

func (a *agent) Config() Config {
	return a.config
}

func (a *agent) CostTracker() CostTracker {
	return a.config.CostTracker
}

// persist saves conversation to store if configured.
func (a *agent) persist() {
	if a.config.Store != nil && a.config.ConversationID != "" && len(a.messages) > 0 {
		_ = a.config.Store.Save(a.config.ConversationID, a.messages)
	}
}

func (a *agent) acquireLock() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return ErrAlreadyRunning{}
	}
	a.running = true
	return nil
}

func (a *agent) releaseLock() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.running = false
}
