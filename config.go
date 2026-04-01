package agentsdk

const (
	DefaultModel     = "claude-sonnet-4-20250514"
	DefaultMaxTokens = 16384
)

// Config holds all settings for an Agent. Use Option functions to customise.
type Config struct {
	// --- core ---
	Provider      Provider
	Model         string
	SystemPrompt  string
	MaxTokens     int
	MaxTurns      int // 0 = unlimited
	Temperature   *float64
	TopP          *float64
	TopK          *int
	StopSequences []string
	Tools         *ToolRegistry
	Executor      ToolExecutor
	Thinking      *ThinkingConfig
	ToolChoice    *ToolChoice

	// --- phase 3 ---
	PermissionHandler PermissionHandler
	Hooks             *Hooks
	CostTracker       CostTracker
	Store             ConversationStore
	ConversationID    string
	Compact           *CompactConfig

	// --- phase 5: prompt engineering ---
	PromptBuilder    *PromptBuilder
	AppendPrompt     string            // appended after system prompt (preset or custom)
	ContextProviders []ContextProvider  // dynamic context injected into first user message
}

func defaultConfig() Config {
	return Config{
		Model:     DefaultModel,
		MaxTokens: DefaultMaxTokens,
		Tools:     NewToolRegistry(),
		Executor:  &ParallelExecutor{},
	}
}

// Option configures an Agent.
type Option func(*Config)

// WithProvider sets the LLM provider.
func WithProvider(p Provider) Option {
	return func(c *Config) { c.Provider = p }
}

// WithModel overrides the default model name.
func WithModel(model string) Option {
	return func(c *Config) { c.Model = model }
}

// WithSystemPrompt sets the system prompt.
func WithSystemPrompt(prompt string) Option {
	return func(c *Config) { c.SystemPrompt = prompt }
}

// WithMaxTokens sets the maximum output tokens per LLM call.
func WithMaxTokens(n int) Option {
	return func(c *Config) { c.MaxTokens = n }
}

// WithMaxTurns limits the number of agentic turns (0 = unlimited).
func WithMaxTurns(n int) Option {
	return func(c *Config) { c.MaxTurns = n }
}

// WithTemperature sets the sampling temperature.
func WithTemperature(t float64) Option {
	return func(c *Config) { c.Temperature = &t }
}

// WithTopP sets the nucleus-sampling threshold.
func WithTopP(p float64) Option {
	return func(c *Config) { c.TopP = &p }
}

// WithTopK sets the top-K sampling parameter.
func WithTopK(k int) Option {
	return func(c *Config) { c.TopK = &k }
}

// WithStopSequences provides custom stop sequences.
func WithStopSequences(seqs ...string) Option {
	return func(c *Config) { c.StopSequences = seqs }
}

// WithTools registers one or more tools on the agent.
func WithTools(tools ...Tool) Option {
	return func(c *Config) {
		for _, t := range tools {
			c.Tools.Register(t)
		}
	}
}

// WithToolRegistry replaces the default tool registry.
func WithToolRegistry(r *ToolRegistry) Option {
	return func(c *Config) { c.Tools = r }
}

// WithToolExecutor replaces the default parallel executor.
func WithToolExecutor(e ToolExecutor) Option {
	return func(c *Config) { c.Executor = e }
}

// WithThinking enables extended thinking with the given token budget.
func WithThinking(budgetTokens int) Option {
	return func(c *Config) {
		c.Thinking = &ThinkingConfig{
			Type:         "enabled",
			BudgetTokens: budgetTokens,
		}
	}
}

// WithAdaptiveThinking enables adaptive thinking where the model dynamically
// decides how much to think. Supported on Claude Sonnet 4.6+ and Opus 4.6+.
func WithAdaptiveThinking() Option {
	return func(c *Config) {
		c.Thinking = &ThinkingConfig{
			Type: "adaptive",
		}
	}
}

// WithToolChoice forces a specific tool selection strategy.
func WithToolChoice(tc ToolChoice) Option {
	return func(c *Config) { c.ToolChoice = &tc }
}

// ---------------------------------------------------------------------------
// Phase 5: prompt engineering options
// ---------------------------------------------------------------------------

// WithPromptBuilder sets a structured PromptBuilder to assemble the system prompt.
// When set, this takes precedence over WithSystemPrompt.
func WithPromptBuilder(b *PromptBuilder) Option {
	return func(c *Config) { c.PromptBuilder = b }
}

// WithClaudeCodePreset configures the agent with Claude Code's system prompt sections.
// Optional append text is added after the preset sections.
func WithClaudeCodePreset(append ...string) Option {
	return func(c *Config) {
		b := ClaudeCodePreset()
		if len(append) > 0 && append[0] != "" {
			b.Append(append[0])
		}
		c.PromptBuilder = b
	}
}

// WithAppendPrompt appends text after the system prompt (works with both
// plain string prompts and PromptBuilder).
func WithAppendPrompt(text string) Option {
	return func(c *Config) { c.AppendPrompt = text }
}

// WithContextProviders adds ContextProviders for dynamic user-message injection.
// Context is resolved once per Run call and injected into the first user message.
func WithContextProviders(providers ...ContextProvider) Option {
	return func(c *Config) { c.ContextProviders = append(c.ContextProviders, providers...) }
}

// ---------------------------------------------------------------------------
// Phase 3 options
// ---------------------------------------------------------------------------

// WithPermissionHandler sets a callback invoked before every tool execution.
// The handler decides whether to allow, deny, or ask about each tool call.
func WithPermissionHandler(h PermissionHandler) Option {
	return func(c *Config) { c.PermissionHandler = h }
}

// WithHooks registers lifecycle hooks for the agent loop.
func WithHooks(h *Hooks) Option {
	return func(c *Config) { c.Hooks = h }
}

// WithCostTracker enables token/cost tracking.
func WithCostTracker(ct CostTracker) Option {
	return func(c *Config) { c.CostTracker = ct }
}

// WithStore sets the conversation persistence backend.
func WithStore(store ConversationStore, conversationID string) Option {
	return func(c *Config) {
		c.Store = store
		c.ConversationID = conversationID
	}
}

// WithCompact enables automatic context-window management.
func WithCompact(maxContextTokens int, opts ...CompactOption) Option {
	return func(c *Config) {
		cfg := &CompactConfig{MaxContextTokens: maxContextTokens, Threshold: 0.8}
		for _, o := range opts {
			o(cfg)
		}
		c.Compact = cfg
	}
}

// CompactOption customises CompactConfig.
type CompactOption func(*CompactConfig)

// CompactThreshold sets the trigger threshold (0.0–1.0).
func CompactThreshold(t float64) CompactOption {
	return func(c *CompactConfig) { c.Threshold = t }
}

// CompactWith sets a custom CompactStrategy.
func CompactWith(s CompactStrategy) CompactOption {
	return func(c *CompactConfig) { c.Strategy = s }
}
