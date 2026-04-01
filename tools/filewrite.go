package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	agentsdk "github.com/chenhg5/go-agent-sdk"
)

// FileWriteTool creates or overwrites a file with the given content.
type FileWriteTool struct{}

type fileWriteInput struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

func (t *FileWriteTool) Definition() agentsdk.ToolSpec {
	return agentsdk.ToolSpec{
		Name:        "file_write",
		Description: "Write content to a file, creating it and any parent directories if they don't exist. Overwrites the file if it already exists. Prefer file_edit for modifying existing files.",
		InputSchema: &agentsdk.JSONSchema{
			Type: "object",
			Properties: map[string]*agentsdk.JSONSchema{
				"file_path": {
					Type:        "string",
					Description: "Path of the file to write.",
				},
				"content": {
					Type:        "string",
					Description: "The complete file content to write.",
				},
			},
			Required: []string{"file_path", "content"},
		},
	}
}

func (t *FileWriteTool) Execute(_ context.Context, call agentsdk.ToolCall) (*agentsdk.ToolResult, error) {
	var in fileWriteInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return &agentsdk.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}
	if in.FilePath == "" {
		return &agentsdk.ToolResult{Content: "file_path is required", IsError: true}, nil
	}

	dir := filepath.Dir(in.FilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return &agentsdk.ToolResult{Content: fmt.Sprintf("cannot create directory %s: %v", dir, err), IsError: true}, nil
	}

	if err := os.WriteFile(in.FilePath, []byte(in.Content), 0644); err != nil {
		return &agentsdk.ToolResult{Content: fmt.Sprintf("write error: %v", err), IsError: true}, nil
	}

	return &agentsdk.ToolResult{Content: fmt.Sprintf("wrote %d bytes to %s", len(in.Content), in.FilePath)}, nil
}
