package agentsdk

import (
	"context"
	"encoding/json"
	"fmt"
)

// SubAgentTool exposes another Agent as a tool. The parent agent can
// delegate tasks to the sub-agent, which runs its own independent loop
// and returns the final text response.
//
//	researcher, _ := agentsdk.New(
//	    agentsdk.WithProvider(provider),
//	    agentsdk.WithSystemPrompt("You are a research assistant."),
//	)
//	mainAgent, _ := agentsdk.New(
//	    agentsdk.WithProvider(provider),
//	    agentsdk.WithTools(&agentsdk.SubAgentTool{
//	        AgentName:   "researcher",
//	        Description: "Delegate research tasks to a specialist.",
//	        SubAgent:    researcher,
//	    }),
//	)
type SubAgentTool struct {
	// AgentName is the tool name exposed to the LLM.
	AgentName string
	// Description is the tool description exposed to the LLM.
	Description string
	// SubAgent is the agent that will execute the delegated task.
	SubAgent Agent
	// MaxTurns limits the sub-agent's turns per invocation (0 = use sub-agent's config).
	MaxTurns int
}

var _ Tool = (*SubAgentTool)(nil)

type subAgentInput struct {
	Task    string `json:"task"`
	Context string `json:"context,omitempty"`
}

func (t *SubAgentTool) Definition() ToolSpec {
	name := t.AgentName
	if name == "" {
		name = "sub_agent"
	}
	desc := t.Description
	if desc == "" {
		desc = "Delegate a task to a sub-agent and get its response."
	}
	return ToolSpec{
		Name:        name,
		Description: desc,
		InputSchema: &JSONSchema{
			Type: "object",
			Properties: map[string]*JSONSchema{
				"task": {
					Type:        "string",
					Description: "The task or question to delegate to the sub-agent.",
				},
				"context": {
					Type:        "string",
					Description: "Optional additional context for the sub-agent.",
				},
			},
			Required: []string{"task"},
		},
	}
}

func (t *SubAgentTool) Execute(ctx context.Context, call ToolCall) (*ToolResult, error) {
	var in subAgentInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return &ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}
	if in.Task == "" {
		return &ToolResult{Content: "task must not be empty", IsError: true}, nil
	}

	prompt := in.Task
	if in.Context != "" {
		prompt = fmt.Sprintf("Context: %s\n\nTask: %s", in.Context, in.Task)
	}

	result, err := t.SubAgent.Run(ctx, prompt)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("sub-agent error: %v", err),
			IsError: true,
		}, nil
	}

	if len(result.Messages) == 0 {
		return &ToolResult{Content: "(no response from sub-agent)", IsError: true}, nil
	}

	last := result.Messages[len(result.Messages)-1]
	content := last.TextContent()
	if content == "" {
		content = fmt.Sprintf("(sub-agent finished with reason: %s)", result.Reason)
	}

	return &ToolResult{Content: content}, nil
}
