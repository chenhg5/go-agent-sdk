package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	agentsdk "github.com/chenhg5/go-agent-sdk"
)

// FileEditTool performs find-and-replace edits on a file.
type FileEditTool struct{}

type fileEditInput struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

func (t *FileEditTool) Definition() agentsdk.ToolSpec {
	return agentsdk.ToolSpec{
		Name:        "file_edit",
		Description: "Replace exact string matches in a file. The old_string must uniquely identify the text to change (unless replace_all is true). Include enough surrounding context to be unambiguous.",
		InputSchema: &agentsdk.JSONSchema{
			Type: "object",
			Properties: map[string]*agentsdk.JSONSchema{
				"file_path": {
					Type:        "string",
					Description: "Path to the file to edit.",
				},
				"old_string": {
					Type:        "string",
					Description: "The exact text to find. Must match the file contents exactly, including whitespace and indentation.",
				},
				"new_string": {
					Type:        "string",
					Description: "The replacement text.",
				},
				"replace_all": {
					Type:        "boolean",
					Description: "If true, replace all occurrences. Default false (requires unique match).",
				},
			},
			Required: []string{"file_path", "old_string", "new_string"},
		},
	}
}

func (t *FileEditTool) Execute(_ context.Context, call agentsdk.ToolCall) (*agentsdk.ToolResult, error) {
	var in fileEditInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return &agentsdk.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}
	if in.FilePath == "" {
		return &agentsdk.ToolResult{Content: "file_path is required", IsError: true}, nil
	}
	if in.OldString == in.NewString {
		return &agentsdk.ToolResult{Content: "old_string and new_string must differ", IsError: true}, nil
	}

	data, err := os.ReadFile(in.FilePath)
	if err != nil {
		return &agentsdk.ToolResult{Content: fmt.Sprintf("read error: %v", err), IsError: true}, nil
	}
	original := string(data)

	count := strings.Count(original, in.OldString)
	if count == 0 {
		return &agentsdk.ToolResult{Content: "old_string not found in file", IsError: true}, nil
	}
	if count > 1 && !in.ReplaceAll {
		return &agentsdk.ToolResult{
			Content: fmt.Sprintf("old_string found %d times — set replace_all=true or provide more context to make it unique", count),
			IsError: true,
		}, nil
	}

	var result string
	if in.ReplaceAll {
		result = strings.ReplaceAll(original, in.OldString, in.NewString)
	} else {
		result = strings.Replace(original, in.OldString, in.NewString, 1)
	}

	info, _ := os.Stat(in.FilePath)
	perm := os.FileMode(0644)
	if info != nil {
		perm = info.Mode().Perm()
	}
	if err := os.WriteFile(in.FilePath, []byte(result), perm); err != nil {
		return &agentsdk.ToolResult{Content: fmt.Sprintf("write error: %v", err), IsError: true}, nil
	}

	msg := fmt.Sprintf("replaced %d occurrence(s) in %s", count, in.FilePath)
	if !in.ReplaceAll {
		msg = fmt.Sprintf("replaced 1 occurrence in %s", in.FilePath)
	}
	return &agentsdk.ToolResult{Content: msg}, nil
}
