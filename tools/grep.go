package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	agentsdk "github.com/chenhg5/go-agent-sdk"
)

const (
	grepMaxMatches  = 500
	grepMaxFileSize = 2 * 1024 * 1024 // 2 MiB
)

// GrepTool searches file contents using regular expressions.
type GrepTool struct{}

type grepInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`    // directory or file
	Include string `json:"include,omitempty"` // file glob filter, e.g. "*.go"
}

func (t *GrepTool) Definition() agentsdk.ToolSpec {
	return agentsdk.ToolSpec{
		Name:        "grep",
		Description: "Search file contents using a regular expression. Returns matching lines with file paths and line numbers.",
		InputSchema: &agentsdk.JSONSchema{
			Type: "object",
			Properties: map[string]*agentsdk.JSONSchema{
				"pattern": {
					Type:        "string",
					Description: "Regular expression pattern to search for.",
				},
				"path": {
					Type:        "string",
					Description: "File or directory to search in. Defaults to current directory.",
				},
				"include": {
					Type:        "string",
					Description: "Glob pattern to filter files, e.g. \"*.go\", \"*.{ts,tsx}\".",
				},
			},
			Required: []string{"pattern"},
		},
	}
}

func (t *GrepTool) Execute(_ context.Context, call agentsdk.ToolCall) (*agentsdk.ToolResult, error) {
	var in grepInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return &agentsdk.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}
	if in.Pattern == "" {
		return &agentsdk.ToolResult{Content: "pattern is required", IsError: true}, nil
	}

	re, err := regexp.Compile(in.Pattern)
	if err != nil {
		return &agentsdk.ToolResult{Content: fmt.Sprintf("invalid regex: %v", err), IsError: true}, nil
	}

	root := in.Path
	if root == "" {
		root = "."
	}

	info, err := os.Stat(root)
	if err != nil {
		return &agentsdk.ToolResult{Content: fmt.Sprintf("cannot access %s: %v", root, err), IsError: true}, nil
	}

	var results []grepMatch
	if info.IsDir() {
		results = searchDir(root, re, in.Include)
	} else {
		results = searchFile(root, re)
	}

	if len(results) == 0 {
		return &agentsdk.ToolResult{Content: "no matches found"}, nil
	}

	truncated := len(results) > grepMaxMatches
	if truncated {
		results = results[:grepMaxMatches]
	}

	var sb strings.Builder
	for _, m := range results {
		fmt.Fprintf(&sb, "%s:%d:%s\n", m.File, m.Line, m.Text)
	}
	if truncated {
		fmt.Fprintf(&sb, "\n... (%d matches shown, more exist)\n", grepMaxMatches)
	} else {
		fmt.Fprintf(&sb, "\n%d match(es)\n", len(results))
	}
	return &agentsdk.ToolResult{Content: sb.String()}, nil
}

type grepMatch struct {
	File string
	Line int
	Text string
}

func searchDir(root string, re *regexp.Regexp, include string) []grepMatch {
	var all []grepMatch
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == "node_modules" || base == "__pycache__" || base == ".venv" || base == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if include != "" && !matchInclude(d.Name(), include) {
			return nil
		}
		if info, _ := d.Info(); info != nil && info.Size() > grepMaxFileSize {
			return nil
		}
		all = append(all, searchFile(path, re)...)
		if len(all) > grepMaxMatches*2 {
			return filepath.SkipAll
		}
		return nil
	})
	return all
}

func searchFile(path string, re *regexp.Regexp) []grepMatch {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var matches []grepMatch
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if re.MatchString(line) {
			matches = append(matches, grepMatch{File: path, Line: lineNum, Text: line})
		}
	}
	return matches
}

func matchInclude(name, include string) bool {
	// Support brace expansion like "*.{go,ts}"
	if strings.Contains(include, "{") && strings.Contains(include, "}") {
		start := strings.Index(include, "{")
		end := strings.Index(include, "}")
		prefix := include[:start]
		suffix := include[end+1:]
		for _, ext := range strings.Split(include[start+1:end], ",") {
			pattern := prefix + strings.TrimSpace(ext) + suffix
			if ok, _ := filepath.Match(pattern, name); ok {
				return true
			}
		}
		return false
	}
	ok, _ := filepath.Match(include, name)
	return ok
}
