package tools

import (
	"fmt"
	"strings"

	agentsdk "github.com/chenhg5/go-agent-sdk"
)

// All built-in tools implement ToolPrompter for rich LLM descriptions.
var (
	_ agentsdk.ToolPrompter = (*BashTool)(nil)
	_ agentsdk.ToolPrompter = (*FileReadTool)(nil)
	_ agentsdk.ToolPrompter = (*FileEditTool)(nil)
	_ agentsdk.ToolPrompter = (*FileWriteTool)(nil)
	_ agentsdk.ToolPrompter = (*GlobTool)(nil)
	_ agentsdk.ToolPrompter = (*GrepTool)(nil)
)

func (t *BashTool) Prompt(ctx agentsdk.PromptContext) string {
	var sb strings.Builder
	sb.WriteString(`Executes a given shell command and returns its output.

The working directory persists between commands, but shell state does not.

IMPORTANT: Avoid using this tool to run commands when a dedicated tool is available:`)

	for _, name := range ctx.Tools {
		switch strings.ToLower(name) {
		case "file_read":
			sb.WriteString("\n- Use file_read instead of cat, head, tail, or sed")
		case "file_edit":
			sb.WriteString("\n- Use file_edit instead of sed or awk")
		case "file_write":
			sb.WriteString("\n- Use file_write instead of cat with heredoc or echo")
		case "glob":
			sb.WriteString("\n- Use glob instead of find or ls")
		case "grep":
			sb.WriteString("\n- Use grep tool instead of grep or rg commands")
		}
	}

	sb.WriteString(`

# Instructions
- Always quote file paths that contain spaces with double quotes
- You may specify an optional timeout in seconds (up to 600). Default timeout is 120 seconds.
- When issuing multiple commands:
  - If independent: make multiple tool calls in parallel
  - If dependent: use '&&' to chain them
  - Use ';' only when you don't care if earlier commands fail
  - DO NOT use newlines to separate commands
- For git commands: prefer new commits rather than amending; never skip hooks unless explicitly asked`)

	return sb.String()
}

func (t *FileReadTool) Prompt(_ agentsdk.PromptContext) string {
	return fmt.Sprintf(`Reads a file from the local filesystem. You can access any file directly by using this tool.
Assume this tool is able to read all files on the machine. If the User provides a path to a file, assume that path is valid.

Usage:
- The file_path parameter must be an absolute path, not a relative path
- By default, it reads the entire file. For large files, use offset and limit to read a specific range
- Results are returned with line numbers starting at 1, using the format: LINE_NUMBER|LINE_CONTENT
- You can optionally specify a line offset and limit (especially handy for long files)
- Maximum file size: %d bytes. Use offset/limit for larger files
- This tool can only read files, not directories. To list a directory, use bash with ls
- If you read a file that exists but has empty contents, a note will be returned`, fileReadMaxSize)
}

func (t *FileEditTool) Prompt(_ agentsdk.PromptContext) string {
	return `Performs exact string replacements in files.

Usage:
- Provide the file path, the exact old_string to find, and the new_string to replace it with
- The old_string MUST uniquely identify the specific instance you want to change. Either provide a larger string with more surrounding context to make it unique, or use replace_all to change every instance
- When editing text, ensure you preserve the exact indentation (tabs/spaces) as it appears
- Only use emojis if the user explicitly requests it
- The edit will FAIL if old_string is not found or is not unique in the file
- If you want to create a new file, use file_write instead
- ALWAYS generate arguments in the order: file_path, old_string, new_string
- Prefer editing existing cells over creating new ones`
}

func (t *FileWriteTool) Prompt(_ agentsdk.PromptContext) string {
	return `Writes content to a file on the local filesystem.

Usage:
- This tool will overwrite the existing file if there is one at the provided path
- Parent directories are automatically created if they don't exist
- ALWAYS prefer editing existing files in the codebase over creating new files
- NEVER proactively create documentation files (*.md) or README files unless explicitly requested
- NEVER write new files unless they're absolutely necessary for achieving your goal`
}

func (t *GlobTool) Prompt(_ agentsdk.PromptContext) string {
	return `Finds files matching a glob pattern.

- Works fast with codebases of any size
- Returns matching file paths sorted by modification time
- Use this tool when you need to find files by name patterns
- Patterns not starting with "**/" are automatically prepended with "**/" to enable recursive searching

Examples:
  - "*.js" → finds all .js files recursively
  - "**/node_modules/**" → finds files inside node_modules
  - "**/test/**/test_*.ts" → finds test files in any test directory`
}

func (t *GrepTool) Prompt(_ agentsdk.PromptContext) string {
	return `Search for a pattern in file contents using regular expressions.

- Supports full regex syntax (e.g., "log.*Error", "function\s+\w+")
- Filter files with glob parameter (e.g., "*.js", "**/*.tsx")
- Output includes file paths and matching lines with line numbers
- Results are capped for responsiveness; use more specific patterns if truncated
- Use this instead of running grep or rg via the bash tool

Examples:
  - Pattern "func.*Error" to find error-handling functions
  - Pattern "TODO|FIXME" to find all todos
  - Pattern "import.*react" with glob "*.tsx" to find React imports`
}
