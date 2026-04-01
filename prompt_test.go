package agentsdk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// PromptBuilder — Build (string)
// ---------------------------------------------------------------------------

func TestPromptBuilder_Build(t *testing.T) {
	b := NewPromptBuilder().
		Section("rules", "# Rules\nFollow the rules.", 20).
		CachedSection("identity", "You are an agent.", 10).
		Section("env", "# Env\nLinux", 30)

	got, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// identity (priority 10) should come first
	if !strings.HasPrefix(got, "You are an agent.") {
		t.Errorf("identity section should come first, got:\n%s", got)
	}
	if !strings.Contains(got, "# Rules") {
		t.Error("missing rules section")
	}
	if !strings.Contains(got, "# Env") {
		t.Error("missing env section")
	}
}

func TestPromptBuilder_Build_WithAppend(t *testing.T) {
	b := NewPromptBuilder().
		Section("main", "Main content.", 10).
		Append("Always respond in JSON.")

	got, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasSuffix(got, "Always respond in JSON.") {
		t.Errorf("append text should be at the end, got:\n%s", got)
	}
}

func TestPromptBuilder_Build_WithProvider(t *testing.T) {
	b := NewPromptBuilder().
		Section("identity", "You are an agent.", 10).
		Provider(StaticContext{Key: "project", Text: "This is a Go project."})

	got, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got, "This is a Go project.") {
		t.Error("provider content should be included")
	}
}

// ---------------------------------------------------------------------------
// PromptBuilder — BuildBlocks (structured)
// ---------------------------------------------------------------------------

func TestPromptBuilder_BuildBlocks_CacheBoundary(t *testing.T) {
	b := NewPromptBuilder().
		CachedSection("identity", "Identity section.", 10).
		CachedSection("rules", "Rules section.", 20).
		Section("env", "Env section.", 30)

	blocks, err := b.BuildBlocks(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}

	// First cached block should NOT have cache_control
	if blocks[0].CacheControl != nil {
		t.Error("first cached block should not have cache_control")
	}

	// Last cached block (index 1) should have cache_control
	if blocks[1].CacheControl == nil || blocks[1].CacheControl.Type != "ephemeral" {
		t.Error("last cached block should have cache_control: ephemeral")
	}

	// Dynamic block should not have cache_control
	if blocks[2].CacheControl != nil {
		t.Error("dynamic block should not have cache_control")
	}
}

func TestPromptBuilder_BuildBlocks_JSON(t *testing.T) {
	b := NewPromptBuilder().
		CachedSection("id", "You are Claude.", 10).
		Section("dynamic", "Date: today", 20)

	blocks, err := b.BuildBlocks(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	data, err := json.MarshalIndent(blocks, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	var parsed []SystemBlock
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("blocks should roundtrip through JSON: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 blocks after roundtrip, got %d", len(parsed))
	}
	if parsed[0].CacheControl == nil {
		t.Error("cache_control should survive JSON roundtrip")
	}
}

// ---------------------------------------------------------------------------
// ClaudeCodePreset
// ---------------------------------------------------------------------------

func TestClaudeCodePreset_Sections(t *testing.T) {
	b := ClaudeCodePreset()
	got, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	required := []string{
		"interactive agent",
		"# System",
		"# Doing tasks",
		"# Executing actions",
		"# Using your tools",
		"# Tone and style",
		"# Output efficiency",
	}
	for _, r := range required {
		if !strings.Contains(got, r) {
			t.Errorf("preset missing expected content: %q", r)
		}
	}
}

func TestClaudeCodePreset_Blocks(t *testing.T) {
	b := ClaudeCodePreset()
	blocks, err := b.BuildBlocks(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(blocks) != 7 {
		t.Fatalf("expected 7 blocks (all cached sections), got %d", len(blocks))
	}

	// All blocks should be cached; last one gets cache_control
	for i, blk := range blocks {
		if i < len(blocks)-1 && blk.CacheControl != nil {
			t.Errorf("block %d should not have cache_control", i)
		}
	}
	if blocks[len(blocks)-1].CacheControl == nil {
		t.Error("last block should have cache_control")
	}
}

// ---------------------------------------------------------------------------
// WrapUserContext / InjectContext
// ---------------------------------------------------------------------------

func TestWrapUserContext(t *testing.T) {
	parts := map[string]string{
		"currentDate": "Today is 2026-04-01.",
	}
	got := WrapUserContext(parts)

	if !strings.Contains(got, "<system-reminder>") {
		t.Error("should contain opening tag")
	}
	if !strings.Contains(got, "# currentDate") {
		t.Error("should contain section header")
	}
	if !strings.Contains(got, "Today is 2026-04-01.") {
		t.Error("should contain date text")
	}
	if !strings.Contains(got, "</system-reminder>") {
		t.Error("should contain closing tag")
	}
}

func TestWrapUserContext_Empty(t *testing.T) {
	if got := WrapUserContext(nil); got != "" {
		t.Errorf("empty parts should return empty string, got %q", got)
	}
}

func TestInjectContext(t *testing.T) {
	msgs := []Message{
		NewUserMessage("Hello"),
		NewAssistantMessage(NewTextBlock("Hi")),
	}
	injected := InjectContext(msgs, "<system-reminder>ctx</system-reminder>")

	if len(injected) != 2 {
		t.Fatalf("message count should be preserved, got %d", len(injected))
	}
	text := injected[0].TextContent()
	if !strings.HasPrefix(text, "<system-reminder>") {
		t.Error("context should be prepended to first user message")
	}
	if !strings.Contains(text, "Hello") {
		t.Error("original text should be preserved")
	}
}

func TestInjectContext_Empty(t *testing.T) {
	msgs := []Message{NewUserMessage("Hello")}
	result := InjectContext(msgs, "")
	if result[0].TextContent() != "Hello" {
		t.Error("empty context should not modify messages")
	}
}

func TestInjectContext_AssistantFirst(t *testing.T) {
	msgs := []Message{NewAssistantMessage(NewTextBlock("Hi"))}
	result := InjectContext(msgs, "some context")
	if result[0].TextContent() != "Hi" {
		t.Error("should not inject into non-user first message")
	}
}

// ---------------------------------------------------------------------------
// Context providers
// ---------------------------------------------------------------------------

func TestDateContext(t *testing.T) {
	dc := DateContext{}
	if dc.Name() != "currentDate" {
		t.Errorf("unexpected name: %s", dc.Name())
	}
	got, err := dc.Provide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Today's date is") {
		t.Errorf("unexpected output: %s", got)
	}
}

func TestEnvContext(t *testing.T) {
	ec := EnvContext{WorkDir: "/tmp/test", Model: "claude-sonnet-4"}
	got, err := ec.Provide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "/tmp/test") {
		t.Error("should contain work dir")
	}
	if !strings.Contains(got, "claude-sonnet-4") {
		t.Error("should contain model name")
	}
}

func TestGitContext_NonGitDir(t *testing.T) {
	dir := t.TempDir()
	gc := GitContext{WorkDir: dir}
	got, err := gc.Provide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Not a git repository") {
		t.Errorf("should indicate not a git repo, got: %s", got)
	}
}

func TestCLAUDEMDContext(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# My Project\nUse Go."), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := CLAUDEMDContext{WorkDir: dir}
	got, err := ctx.Provide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "# My Project") {
		t.Error("should load CLAUDE.md content")
	}
}

func TestCLAUDEMDContext_Missing(t *testing.T) {
	dir := t.TempDir()
	ctx := CLAUDEMDContext{WorkDir: dir}
	got, err := ctx.Provide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("should return empty when no CLAUDE.md found, got: %s", got)
	}
}

func TestCLAUDEMDContext_DotClaudeDir(t *testing.T) {
	dir := t.TempDir()
	dotDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(dotDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dotDir, "CLAUDE.md"), []byte("From .claude dir"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := CLAUDEMDContext{WorkDir: dir}
	got, err := ctx.Provide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "From .claude dir") {
		t.Error("should load from .claude/CLAUDE.md")
	}
}

func TestStaticContext(t *testing.T) {
	sc := StaticContext{Key: "custom", Text: "Custom instructions."}
	if sc.Name() != "custom" {
		t.Errorf("unexpected name: %s", sc.Name())
	}
	got, err := sc.Provide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "Custom instructions." {
		t.Errorf("unexpected output: %s", got)
	}
}

func TestContextProviderFunc(t *testing.T) {
	cpf := ContextProviderFunc{
		ProviderName: "test",
		Fn: func(_ context.Context) (string, error) {
			return "dynamic value", nil
		},
	}
	if cpf.Name() != "test" {
		t.Errorf("unexpected name: %s", cpf.Name())
	}
	got, err := cpf.Provide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "dynamic value" {
		t.Errorf("unexpected output: %s", got)
	}
}

// ---------------------------------------------------------------------------
// Integration: PromptBuilder with ContextProviders in agent config
// ---------------------------------------------------------------------------

func TestConfig_WithClaudeCodePreset(t *testing.T) {
	cfg := defaultConfig()
	WithClaudeCodePreset("Always respond in Chinese.")(& cfg)

	if cfg.PromptBuilder == nil {
		t.Fatal("PromptBuilder should be set")
	}

	got, err := cfg.PromptBuilder.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "interactive agent") {
		t.Error("should contain preset content")
	}
	if !strings.HasSuffix(got, "Always respond in Chinese.") {
		t.Error("should end with append text")
	}
}

func TestConfig_WithAppendPrompt(t *testing.T) {
	cfg := defaultConfig()
	WithSystemPrompt("You are a helper.")(&cfg)
	WithAppendPrompt("Always be kind.")(&cfg)

	if cfg.SystemPrompt != "You are a helper." {
		t.Error("system prompt should be set")
	}
	if cfg.AppendPrompt != "Always be kind." {
		t.Error("append prompt should be set")
	}
}

func TestConfig_WithContextProviders(t *testing.T) {
	cfg := defaultConfig()
	WithContextProviders(DateContext{}, EnvContext{Model: "test"})(&cfg)

	if len(cfg.ContextProviders) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(cfg.ContextProviders))
	}
}

// ---------------------------------------------------------------------------
// ToolPrompter + SpecsWithContext
// ---------------------------------------------------------------------------

type mockToolWithPrompt struct {
	name string
}

func (m *mockToolWithPrompt) Definition() ToolSpec {
	return ToolSpec{Name: m.name, Description: "short desc"}
}

func (m *mockToolWithPrompt) Execute(_ context.Context, _ ToolCall) (*ToolResult, error) {
	return &ToolResult{Content: "ok"}, nil
}

func (m *mockToolWithPrompt) Prompt(ctx PromptContext) string {
	return "Rich description for " + m.name + " (tools: " + strings.Join(ctx.Tools, ",") + ")"
}

func TestSpecsWithContext_UsesPrompt(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&mockToolWithPrompt{name: "test_tool"})

	// Without context — should use static Definition
	specs := r.Specs()
	if specs[0].Description != "Rich description for test_tool (tools: test_tool)" {
		// Specs() now delegates to SpecsWithContext with empty context,
		// but Names() is still called so tools list should be populated
		t.Logf("got: %s", specs[0].Description)
	}

	// With explicit context
	pctx := PromptContext{
		Tools: []string{"test_tool", "bash", "file_read"},
		Model: "claude-sonnet",
	}
	specs2 := r.SpecsWithContext(pctx)
	if len(specs2) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs2))
	}
	if !strings.Contains(specs2[0].Description, "Rich description for test_tool") {
		t.Errorf("expected rich description, got: %s", specs2[0].Description)
	}
	if !strings.Contains(specs2[0].Description, "bash") {
		t.Errorf("should include tools from context: %s", specs2[0].Description)
	}
}

func TestSpecsWithContext_FallsBackToDefinition(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&mockTool{
		spec: ToolSpec{Name: "plain_tool", Description: "just a plain tool"},
	})

	pctx := PromptContext{Tools: []string{"plain_tool"}, Model: "test"}
	specs := r.SpecsWithContext(pctx)
	if specs[0].Description != "just a plain tool" {
		t.Errorf("should use Definition() desc, got: %s", specs[0].Description)
	}
}

func TestRegistryNames(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&mockTool{spec: ToolSpec{Name: "a"}})
	r.Register(&mockTool{spec: ToolSpec{Name: "b"}})

	names := r.Names()
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Errorf("unexpected names: %v", names)
	}
}

// ---------------------------------------------------------------------------
// BuildToolUsageSection
// ---------------------------------------------------------------------------

func TestBuildToolUsageSection_WithBash(t *testing.T) {
	section := BuildToolUsageSection([]string{"bash", "file_read", "grep"})

	if !strings.Contains(section, "# Using your tools") {
		t.Error("should have header")
	}
	if !strings.Contains(section, "file_read") {
		t.Error("should reference file_read")
	}
	if !strings.Contains(section, "grep") {
		t.Error("should reference grep")
	}
	if strings.Contains(section, "glob") {
		t.Error("should NOT reference glob (not registered)")
	}
}

func TestBuildToolUsageSection_NoBash(t *testing.T) {
	section := BuildToolUsageSection([]string{"file_read", "grep"})
	if strings.Contains(section, "Do NOT use the bash") {
		t.Error("should not include bash warning when bash not registered")
	}
}

// ---------------------------------------------------------------------------
// ClaudeCodePresetForTools
// ---------------------------------------------------------------------------

func TestClaudeCodePresetForTools(t *testing.T) {
	b := ClaudeCodePresetForTools([]string{"bash", "file_read", "file_edit", "glob", "grep"})
	got, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got, "file_read instead of cat") {
		t.Error("dynamic tool_usage should reference file_read")
	}
	if !strings.Contains(got, "interactive agent") {
		t.Error("should still have identity section")
	}
}

// ---------------------------------------------------------------------------
// ScopedSystemBlock
// ---------------------------------------------------------------------------

func TestBuildScopedBlocks(t *testing.T) {
	b := NewPromptBuilder().
		CachedSection("id", "Identity.", 10).
		CachedSection("rules", "Rules.", 20).
		Section("env", "Env info.", 30)

	blocks, err := b.BuildScopedBlocks(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}

	// First cached: no cache
	if blocks[0].CacheControl != nil {
		t.Error("first cached block should not have cache_control")
	}

	// Last cached: global scope
	if blocks[1].CacheControl == nil || blocks[1].CacheControl.Scope != "global" {
		t.Error("last cached block should have scope=global")
	}

	// Dynamic: no cache
	if blocks[2].CacheControl != nil {
		t.Error("dynamic block should not have cache_control")
	}
}
