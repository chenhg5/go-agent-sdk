package agentsdk

// CompactStrategy decides how to shorten a conversation when the context
// window is approaching its limit. Implement this interface for custom
// compaction logic (e.g. LLM-based summarisation).
type CompactStrategy interface {
	// Compact receives the full message history and returns a shortened
	// version. The returned slice must preserve correct role alternation.
	Compact(messages []Message) []Message
}

// CompactConfig controls automatic context-window management.
type CompactConfig struct {
	// MaxContextTokens is the model's context window size.
	// When EstimateTokens(messages) exceeds Threshold * MaxContextTokens,
	// the strategy is applied.
	MaxContextTokens int
	// Threshold triggers compaction (0.0–1.0). Default 0.8.
	Threshold float64
	// Strategy performs the actual compaction. If nil, TailCompact is used.
	Strategy CompactStrategy
}

// ---------------------------------------------------------------------------
// Token estimation (heuristic)
// ---------------------------------------------------------------------------

// EstimateTokens returns a rough token count for a message list.
// Uses the ~4 chars/token heuristic for English text.
func EstimateTokens(messages []Message) int {
	total := 0
	for _, m := range messages {
		total += 4 // per-message overhead (role, separators)
		for _, b := range m.Content {
			switch b.Type {
			case "text":
				total += charTokens(b.Text)
			case "thinking":
				total += charTokens(b.Thinking)
			case "tool_use":
				total += 3 + charTokens(b.Name) + len(b.Input)/4
			case "tool_result":
				total += 3 + charTokens(b.Content)
			}
		}
	}
	return total
}

func charTokens(s string) int {
	n := len(s) / 4
	if n == 0 && len(s) > 0 {
		n = 1
	}
	return n
}

// needsCompact checks if the current messages exceed the compaction threshold.
func needsCompact(cfg *CompactConfig, messages []Message) bool {
	if cfg == nil || cfg.MaxContextTokens <= 0 {
		return false
	}
	threshold := cfg.Threshold
	if threshold <= 0 || threshold > 1 {
		threshold = 0.8
	}
	estimated := EstimateTokens(messages)
	return estimated > int(float64(cfg.MaxContextTokens)*threshold)
}

// applyCompact runs the configured strategy (or TailCompact as fallback).
func applyCompact(cfg *CompactConfig, messages []Message) []Message {
	if cfg.Strategy != nil {
		return cfg.Strategy.Compact(messages)
	}
	return (&TailCompact{Keep: 20}).Compact(messages)
}

// ---------------------------------------------------------------------------
// Built-in strategies
// ---------------------------------------------------------------------------

// TailCompact keeps only the last N messages, discarding older context.
// Simple but effective when combined with a system prompt that contains
// the project background.
type TailCompact struct {
	Keep int
}

func (c *TailCompact) Compact(messages []Message) []Message {
	if len(messages) <= c.Keep {
		return messages
	}
	return messages[len(messages)-c.Keep:]
}

// SlidingWindowCompact keeps the first K messages (anchors) and the last
// N messages, dropping everything in between.
type SlidingWindowCompact struct {
	KeepFirst int
	KeepLast  int
}

func (c *SlidingWindowCompact) Compact(messages []Message) []Message {
	total := c.KeepFirst + c.KeepLast
	if len(messages) <= total {
		return messages
	}
	head := messages[:c.KeepFirst]
	tail := messages[len(messages)-c.KeepLast:]
	out := make([]Message, 0, total)
	out = append(out, head...)
	out = append(out, tail...)
	return out
}
