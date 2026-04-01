package agentsdk

import "sync"

// ToolRegistry holds a named collection of tools.
// It is safe for concurrent reads; writes (Register/Remove) are serialised.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
	order []string // preserves insertion order for stable API serialisation
}

// NewToolRegistry creates an empty registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]Tool)}
}

// Register adds a tool. If a tool with the same name exists it is replaced.
func (r *ToolRegistry) Register(t Tool) {
	name := t.Definition().Name
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; !exists {
		r.order = append(r.order, name)
	}
	r.tools[name] = t
}

// Remove unregisters a tool by name.
func (r *ToolRegistry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

// Get retrieves a tool by name.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// All returns every registered tool in insertion order.
func (r *ToolRegistry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		if t, ok := r.tools[name]; ok {
			out = append(out, t)
		}
	}
	return out
}

// Names returns all registered tool names in insertion order.
func (r *ToolRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Specs returns the API-facing ToolSpec list in stable order.
func (r *ToolRegistry) Specs() []ToolSpec {
	return r.SpecsWithContext(PromptContext{})
}

// SpecsWithContext returns tool specs, using ToolPrompter.Prompt()
// for tools that implement it. This provides rich, context-aware
// descriptions to the LLM instead of static one-liners.
func (r *ToolRegistry) SpecsWithContext(pctx PromptContext) []ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ToolSpec, 0, len(r.order))
	for _, name := range r.order {
		t, ok := r.tools[name]
		if !ok {
			continue
		}
		spec := t.Definition()
		if p, yes := t.(ToolPrompter); yes {
			spec.Description = p.Prompt(pctx)
		}
		out = append(out, spec)
	}
	return out
}

// Len returns the number of registered tools.
func (r *ToolRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}
