package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	agentsdk "github.com/chenhg5/go-agent-sdk"
)

const globMaxResults = 1000

// GlobTool finds files matching a glob pattern, with support for recursive
// "**" matching.
type GlobTool struct{}

type globInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"` // root directory; default "."
}

func (t *GlobTool) Definition() agentsdk.ToolSpec {
	return agentsdk.ToolSpec{
		Name:        "glob",
		Description: "Find files matching a glob pattern. Supports ** for recursive matching. Example: \"**/*.go\" finds all Go files.",
		InputSchema: &agentsdk.JSONSchema{
			Type: "object",
			Properties: map[string]*agentsdk.JSONSchema{
				"pattern": {
					Type:        "string",
					Description: "Glob pattern. Patterns not starting with \"**/\" are automatically prepended with \"**/\" for recursive search.",
				},
				"path": {
					Type:        "string",
					Description: "Root directory to search from. Defaults to current directory.",
				},
			},
			Required: []string{"pattern"},
		},
	}
}

func (t *GlobTool) Execute(_ context.Context, call agentsdk.ToolCall) (*agentsdk.ToolResult, error) {
	var in globInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return &agentsdk.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}
	if in.Pattern == "" {
		return &agentsdk.ToolResult{Content: "pattern is required", IsError: true}, nil
	}

	root := in.Path
	if root == "" {
		root = "."
	}

	pattern := in.Pattern
	if !strings.HasPrefix(pattern, "**/") && !strings.HasPrefix(pattern, "/") && !strings.HasPrefix(pattern, ".") {
		pattern = "**/" + pattern
	}

	matches, err := recursiveGlob(root, pattern)
	if err != nil {
		return &agentsdk.ToolResult{Content: fmt.Sprintf("glob error: %v", err), IsError: true}, nil
	}

	sort.Strings(matches)

	if len(matches) == 0 {
		return &agentsdk.ToolResult{Content: "no files matched"}, nil
	}

	truncated := false
	if len(matches) > globMaxResults {
		matches = matches[:globMaxResults]
		truncated = true
	}

	var sb strings.Builder
	for _, m := range matches {
		sb.WriteString(m)
		sb.WriteByte('\n')
	}
	if truncated {
		fmt.Fprintf(&sb, "... (%d results shown, more exist)\n", globMaxResults)
	} else {
		fmt.Fprintf(&sb, "\n%d file(s) found\n", len(matches))
	}
	return &agentsdk.ToolResult{Content: sb.String()}, nil
}

// recursiveGlob walks root and matches every file path against pattern.
// It supports ** (double-star) for recursive directory matching.
func recursiveGlob(root, pattern string) ([]string, error) {
	// Fast path: no ** → use standard filepath.Glob
	if !strings.Contains(pattern, "**") {
		return filepath.Glob(filepath.Join(root, pattern))
	}

	var matches []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == "node_modules" || base == "__pycache__" || base == ".venv" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if matchDoublestar(pattern, rel) {
			matches = append(matches, path)
		}
		return nil
	})
	return matches, err
}

// matchDoublestar performs simplified double-star glob matching.
func matchDoublestar(pattern, name string) bool {
	// Normalise separators
	pattern = filepath.ToSlash(pattern)
	name = filepath.ToSlash(name)

	// Split on "**"
	parts := strings.Split(pattern, "**")
	if len(parts) == 1 {
		ok, _ := filepath.Match(pattern, name)
		return ok
	}

	// "**/" prefix: match any leading path
	if len(parts) == 2 && parts[0] == "" {
		suffix := strings.TrimPrefix(parts[1], "/")
		// Match suffix against the basename or any sub-path
		segments := strings.Split(name, "/")
		for i := range segments {
			candidate := strings.Join(segments[i:], "/")
			if ok, _ := filepath.Match(suffix, candidate); ok {
				return true
			}
		}
		return false
	}

	// General case: prefix**suffix
	if len(parts) == 2 {
		prefix := strings.TrimSuffix(parts[0], "/")
		suffix := strings.TrimPrefix(parts[1], "/")
		if prefix != "" && !strings.HasPrefix(name, prefix+"/") && name != prefix {
			return false
		}
		trimmed := name
		if prefix != "" {
			trimmed = strings.TrimPrefix(name, prefix+"/")
		}
		segments := strings.Split(trimmed, "/")
		for i := range segments {
			candidate := strings.Join(segments[i:], "/")
			if ok, _ := filepath.Match(suffix, candidate); ok {
				return true
			}
		}
		return false
	}

	// Fallback: just match the basename
	ok, _ := filepath.Match(filepath.Base(pattern), filepath.Base(name))
	return ok
}
