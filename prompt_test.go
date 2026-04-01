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
