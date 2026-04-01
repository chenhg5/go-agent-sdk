package agentsdk

import (
	"context"
	"testing"
)

func TestSubAgentTool_Definition(t *testing.T) {
	sub := &SubAgentTool{
		AgentName:   "researcher",
		Description: "Research things",
	}

	spec := sub.Definition()
	if spec.Name != "researcher" {
		t.Errorf("name = %q", spec.Name)
	}
	if spec.Description != "Research things" {
		t.Errorf("description = %q", spec.Description)
	}
	if spec.InputSchema == nil || len(spec.InputSchema.Required) == 0 {
		t.Error("should require 'task' field")
	}
}

func TestSubAgentTool_Defaults(t *testing.T) {
	sub := &SubAgentTool{}
	spec := sub.Definition()
	if spec.Name != "sub_agent" {
		t.Errorf("default name = %q, want 'sub_agent'", spec.Name)
	}
}

func TestSubAgentTool_Execute(t *testing.T) {
	subProvider := &mockProvider{responses: [][]StreamEvent{
		mockTextEvents("The answer is 42."),
	}}
	subAgent, err := New(WithProvider(subProvider))
	if err != nil {
		t.Fatal(err)
	}

	sub := &SubAgentTool{
		AgentName:   "oracle",
		Description: "Answers questions",
		SubAgent:    subAgent,
	}

	// Wrap the sub-agent as a tool for the main agent
	mainProvider := &mockProvider{responses: [][]StreamEvent{
		mockToolUseEvents("c1", "oracle", `{"task":"What is the meaning of life?"}`),
		mockTextEvents("The oracle says: 42."),
	}}

	mainAgent, err := New(WithProvider(mainProvider), WithTools(sub))
	if err != nil {
		t.Fatal(err)
	}

	result, err := mainAgent.Run(context.Background(), "Ask the oracle")
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != ReasonEndTurn {
		t.Fatalf("reason = %q", result.Reason)
	}

	// The sub-agent should have been called
	subMsgs := subAgent.Messages()
	if len(subMsgs) == 0 {
		t.Error("sub-agent should have messages")
	}

	// The main agent should have the final response
	last := result.Messages[len(result.Messages)-1]
	if last.TextContent() != "The oracle says: 42." {
		t.Errorf("final = %q", last.TextContent())
	}
}

func TestSubAgentTool_EmptyTask(t *testing.T) {
	sub := &SubAgentTool{SubAgent: nil}
	result, err := sub.Execute(context.Background(), ToolCall{
		Input: []byte(`{"task":""}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("empty task should be an error")
	}
}

func TestSubAgentTool_WithContext(t *testing.T) {
	subProvider := &mockProvider{responses: [][]StreamEvent{
		mockTextEvents("Found: relevant info"),
	}}
	subAgent, err := New(WithProvider(subProvider))
	if err != nil {
		t.Fatal(err)
	}

	sub := &SubAgentTool{AgentName: "helper", SubAgent: subAgent}

	result, err := sub.Execute(context.Background(), ToolCall{
		Input: []byte(`{"task":"find info","context":"in the docs folder"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}
	if result.Content != "Found: relevant info" {
		t.Errorf("content = %q", result.Content)
	}

	// Verify the prompt includes context
	recorded := subProvider.recorded()
	if len(recorded) == 0 {
		t.Fatal("no recorded calls")
	}
	prompt := recorded[0].Messages[0].TextContent()
	if prompt == "" {
		t.Error("prompt should not be empty")
	}
}
