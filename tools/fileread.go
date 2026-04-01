package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	agentsdk "github.com/chenhg5/go-agent-sdk"
)

const fileReadMaxSize = 1024 * 1024 // 1 MiB default

// FileReadTool reads a file and returns its contents with line numbers.
type FileReadTool struct {
	// MaxFileSize overrides the default 1 MiB read limit.
	MaxFileSize int
}

type fileReadInput struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset,omitempty"` // 1-based line number
	Limit    int    `json:"limit,omitempty"`  // number of lines to return
}

func (t *FileReadTool) Definition() agentsdk.ToolSpec {
	return agentsdk.ToolSpec{
		Name:        "file_read",
		Description: "Read a file from the local filesystem. Returns contents with line numbers. For large files use offset and limit to read a specific range.",
		InputSchema: &agentsdk.JSONSchema{
			Type: "object",
			Properties: map[string]*agentsdk.JSONSchema{
				"file_path": {
					Type:        "string",
					Description: "Absolute or relative path to the file.",
				},
				"offset": {
					Type:        "integer",
					Description: "Start reading from this line number (1-based). Defaults to 1.",
				},
				"limit": {
					Type:        "integer",
					Description: "Maximum number of lines to return. 0 means read entire file.",
				},
			},
			Required: []string{"file_path"},
		},
	}
}

func (t *FileReadTool) Execute(_ context.Context, call agentsdk.ToolCall) (*agentsdk.ToolResult, error) {
	var in fileReadInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return &agentsdk.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}
	if in.FilePath == "" {
		return &agentsdk.ToolResult{Content: "file_path is required", IsError: true}, nil
	}

	info, err := os.Stat(in.FilePath)
	if err != nil {
		return &agentsdk.ToolResult{Content: fmt.Sprintf("cannot access %s: %v", in.FilePath, err), IsError: true}, nil
	}
	if info.IsDir() {
		return &agentsdk.ToolResult{Content: fmt.Sprintf("%s is a directory, not a file", in.FilePath), IsError: true}, nil
	}

	maxSize := fileReadMaxSize
	if t.MaxFileSize > 0 {
		maxSize = t.MaxFileSize
	}
	if info.Size() > int64(maxSize) {
		return &agentsdk.ToolResult{
			Content: fmt.Sprintf("file is too large (%d bytes, limit %d). Use offset/limit to read a portion.", info.Size(), maxSize),
			IsError: true,
		}, nil
	}

	data, err := os.ReadFile(in.FilePath)
	if err != nil {
		return &agentsdk.ToolResult{Content: fmt.Sprintf("read error: %v", err), IsError: true}, nil
	}

	if isBinary(data) {
		return &agentsdk.ToolResult{
			Content: fmt.Sprintf("%s appears to be a binary file (%d bytes)", in.FilePath, len(data)),
			IsError: true,
		}, nil
	}

	lines := strings.Split(string(data), "\n")
	// Handle trailing newline: if the last element is empty, remove it
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	offset := in.Offset
	if offset < 1 {
		offset = 1
	}
	if offset > len(lines) {
		return &agentsdk.ToolResult{
			Content: fmt.Sprintf("offset %d exceeds file length (%d lines)", offset, len(lines)),
			IsError: true,
		}, nil
	}

	end := len(lines)
	if in.Limit > 0 && offset-1+in.Limit < end {
		end = offset - 1 + in.Limit
	}

	var sb strings.Builder
	for i := offset - 1; i < end; i++ {
		fmt.Fprintf(&sb, "%6d|%s\n", i+1, lines[i])
	}
	return &agentsdk.ToolResult{Content: sb.String()}, nil
}

func isBinary(data []byte) bool {
	n := 512
	if len(data) < n {
		n = len(data)
	}
	for _, b := range data[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}
