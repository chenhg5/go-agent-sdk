package agentsdk

import "sync"

// CostTracker accumulates token usage and estimates monetary cost across
// the lifetime of an agent. Swap the implementation for custom pricing.
type CostTracker interface {
	// AddUsage records tokens consumed by a single API call.
	AddUsage(model string, usage Usage)
	// TotalCost returns the estimated cumulative cost in USD.
	TotalCost() float64
	// TotalUsage returns the aggregate token counts.
	TotalUsage() Usage
	// ByModel returns per-model breakdowns.
	ByModel() map[string]ModelCost
	// Reset zeroes all counters.
	Reset()
}

// ModelCost pairs token counts with an estimated dollar cost.
type ModelCost struct {
	Usage Usage
	Cost  float64
}

// ModelPrice defines per-million-token pricing for a single model.
type ModelPrice struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

// PriceTable maps model names to their pricing.
type PriceTable map[string]ModelPrice

// DefaultPriceTable contains pricing for well-known Claude models.
var DefaultPriceTable = PriceTable{
	"claude-sonnet-4-20250514":    {InputPerMillion: 3.0, OutputPerMillion: 15.0},
	"claude-opus-4-20250514":      {InputPerMillion: 15.0, OutputPerMillion: 75.0},
	"claude-haiku-3-20250307":     {InputPerMillion: 0.25, OutputPerMillion: 1.25},
	"claude-3-5-sonnet-20241022":  {InputPerMillion: 3.0, OutputPerMillion: 15.0},
	"claude-3-5-haiku-20241022":   {InputPerMillion: 0.80, OutputPerMillion: 4.0},
}

// ---------------------------------------------------------------------------
// Default implementation
// ---------------------------------------------------------------------------

type defaultTracker struct {
	mu     sync.Mutex
	prices PriceTable
	total  Usage
	models map[string]*trackerEntry
}

type trackerEntry struct {
	usage Usage
	cost  float64
}

// NewCostTracker creates a CostTracker with the given price table.
// Pass nil to use DefaultPriceTable.
func NewCostTracker(prices PriceTable) CostTracker {
	if prices == nil {
		prices = DefaultPriceTable
	}
	return &defaultTracker{
		prices: prices,
		models: make(map[string]*trackerEntry),
	}
}

func (t *defaultTracker) AddUsage(model string, usage Usage) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.total = t.total.Add(usage)

	entry, ok := t.models[model]
	if !ok {
		entry = &trackerEntry{}
		t.models[model] = entry
	}
	entry.usage = entry.usage.Add(usage)

	if price, ok := t.prices[model]; ok {
		cost := float64(usage.InputTokens)/1_000_000*price.InputPerMillion +
			float64(usage.OutputTokens)/1_000_000*price.OutputPerMillion
		entry.cost += cost
	}
}

func (t *defaultTracker) TotalCost() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	var total float64
	for _, e := range t.models {
		total += e.cost
	}
	return total
}

func (t *defaultTracker) TotalUsage() Usage {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.total
}

func (t *defaultTracker) ByModel() map[string]ModelCost {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]ModelCost, len(t.models))
	for k, v := range t.models {
		out[k] = ModelCost{Usage: v.usage, Cost: v.cost}
	}
	return out
}

func (t *defaultTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.total = Usage{}
	t.models = make(map[string]*trackerEntry)
}
