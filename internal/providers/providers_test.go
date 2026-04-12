package providers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DojoGenesis/cli/internal/providers"
)

// ─── LoadAPIKeys ──────────────────────────────────────────────────────────────

func TestLoadAPIKeys_AllSet(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	t.Setenv("OPENAI_API_KEY", "sk-openai-test")
	t.Setenv("KIMI_API_KEY", "kimi-test")
	t.Setenv("GOOGLE_API_KEY", "google-test")

	keys := providers.LoadAPIKeys()

	if keys.AnthropicKey != "sk-ant-test" {
		t.Errorf("AnthropicKey: got %q, want %q", keys.AnthropicKey, "sk-ant-test")
	}
	if keys.OpenAIKey != "sk-openai-test" {
		t.Errorf("OpenAIKey: got %q, want %q", keys.OpenAIKey, "sk-openai-test")
	}
	if keys.KimiKey != "kimi-test" {
		t.Errorf("KimiKey: got %q, want %q", keys.KimiKey, "kimi-test")
	}
	if keys.GeminiKey != "google-test" {
		t.Errorf("GeminiKey: got %q, want %q", keys.GeminiKey, "google-test")
	}
}

func TestLoadAPIKeys_NoneSet(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("KIMI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")

	keys := providers.LoadAPIKeys()

	if keys.AnthropicKey != "" {
		t.Errorf("AnthropicKey: expected empty, got %q", keys.AnthropicKey)
	}
	if keys.OpenAIKey != "" {
		t.Errorf("OpenAIKey: expected empty, got %q", keys.OpenAIKey)
	}
	if keys.KimiKey != "" {
		t.Errorf("KimiKey: expected empty, got %q", keys.KimiKey)
	}
	if keys.GeminiKey != "" {
		t.Errorf("GeminiKey: expected empty, got %q", keys.GeminiKey)
	}
}

func TestLoadAPIKeys_PartialSet_AnthropicOnly(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-partial")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("KIMI_API_KEY", "")

	keys := providers.LoadAPIKeys()

	if keys.AnthropicKey != "sk-ant-partial" {
		t.Errorf("AnthropicKey: got %q, want %q", keys.AnthropicKey, "sk-ant-partial")
	}
	if keys.OpenAIKey != "" {
		t.Errorf("OpenAIKey: expected empty, got %q", keys.OpenAIKey)
	}
	if keys.KimiKey != "" {
		t.Errorf("KimiKey: expected empty, got %q", keys.KimiKey)
	}
}

func TestLoadAPIKeys_PartialSet_OpenAIOnly(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "sk-openai-partial")
	t.Setenv("KIMI_API_KEY", "")

	keys := providers.LoadAPIKeys()

	if keys.AnthropicKey != "" {
		t.Errorf("AnthropicKey: expected empty, got %q", keys.AnthropicKey)
	}
	if keys.OpenAIKey != "sk-openai-partial" {
		t.Errorf("OpenAIKey: got %q, want %q", keys.OpenAIKey, "sk-openai-partial")
	}
	if keys.KimiKey != "" {
		t.Errorf("KimiKey: expected empty, got %q", keys.KimiKey)
	}
}

func TestLoadAPIKeys_PartialSet_KimiOnly(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("KIMI_API_KEY", "kimi-partial")

	keys := providers.LoadAPIKeys()

	if keys.AnthropicKey != "" {
		t.Errorf("AnthropicKey: expected empty, got %q", keys.AnthropicKey)
	}
	if keys.OpenAIKey != "" {
		t.Errorf("OpenAIKey: expected empty, got %q", keys.OpenAIKey)
	}
	if keys.KimiKey != "kimi-partial" {
		t.Errorf("KimiKey: got %q, want %q", keys.KimiKey, "kimi-partial")
	}
}

// ─── HasDirectAccess ──────────────────────────────────────────────────────────

func TestHasDirectAccess_TrueWhenAnthropicSet(t *testing.T) {
	keys := providers.APIKeys{AnthropicKey: "sk-ant-x"}
	if !keys.HasDirectAccess() {
		t.Error("expected HasDirectAccess() = true when AnthropicKey is set")
	}
}

func TestHasDirectAccess_TrueWhenOpenAISet(t *testing.T) {
	keys := providers.APIKeys{OpenAIKey: "sk-openai-x"}
	if !keys.HasDirectAccess() {
		t.Error("expected HasDirectAccess() = true when OpenAIKey is set")
	}
}

func TestHasDirectAccess_TrueWhenKimiSet(t *testing.T) {
	keys := providers.APIKeys{KimiKey: "kimi-x"}
	if !keys.HasDirectAccess() {
		t.Error("expected HasDirectAccess() = true when KimiKey is set")
	}
}

func TestHasDirectAccess_TrueWhenGoogleSet(t *testing.T) {
	keys := providers.APIKeys{GeminiKey: "google-x"}
	if !keys.HasDirectAccess() {
		t.Error("expected HasDirectAccess() = true when GeminiKey is set")
	}
}

func TestHasDirectAccess_TrueWhenAllSet(t *testing.T) {
	keys := providers.APIKeys{
		AnthropicKey: "sk-ant-x",
		OpenAIKey:    "sk-openai-x",
		KimiKey:      "kimi-x",
	}
	if !keys.HasDirectAccess() {
		t.Error("expected HasDirectAccess() = true when all keys are set")
	}
}

func TestHasDirectAccess_FalseWhenNoneSet(t *testing.T) {
	keys := providers.APIKeys{}
	if keys.HasDirectAccess() {
		t.Error("expected HasDirectAccess() = false when no keys are set")
	}
}

// ─── KeyForProvider ───────────────────────────────────────────────────────────

func TestKeyForProvider_Anthropic(t *testing.T) {
	keys := providers.APIKeys{AnthropicKey: "sk-ant-key"}
	got := keys.KeyForProvider("anthropic")
	if got != "sk-ant-key" {
		t.Errorf("KeyForProvider(%q): got %q, want %q", "anthropic", got, "sk-ant-key")
	}
}

func TestKeyForProvider_OpenAI(t *testing.T) {
	keys := providers.APIKeys{OpenAIKey: "sk-openai-key"}
	got := keys.KeyForProvider("openai")
	if got != "sk-openai-key" {
		t.Errorf("KeyForProvider(%q): got %q, want %q", "openai", got, "sk-openai-key")
	}
}

func TestKeyForProvider_Kimi(t *testing.T) {
	keys := providers.APIKeys{KimiKey: "kimi-key"}
	got := keys.KeyForProvider("kimi")
	if got != "kimi-key" {
		t.Errorf("KeyForProvider(%q): got %q, want %q", "kimi", got, "kimi-key")
	}
}

func TestKeyForProvider_Google(t *testing.T) {
	keys := providers.APIKeys{GeminiKey: "google-key"}
	got := keys.KeyForProvider("google")
	if got != "google-key" {
		t.Errorf("KeyForProvider(%q): got %q, want %q", "google", got, "google-key")
	}
}

func TestKeyForProvider_Unknown(t *testing.T) {
	keys := providers.APIKeys{AnthropicKey: "sk-ant-key", OpenAIKey: "sk-openai-key"}
	got := keys.KeyForProvider("unknown-provider")
	if got != "" {
		t.Errorf("KeyForProvider(%q): got %q, want empty string", "unknown-provider", got)
	}
}

func TestKeyForProvider_Empty(t *testing.T) {
	keys := providers.APIKeys{AnthropicKey: "sk-ant-key"}
	got := keys.KeyForProvider("")
	if got != "" {
		t.Errorf("KeyForProvider(%q): got %q, want empty string", "", got)
	}
}

// ─── InferProvider ────────────────────────────────────────────────────────────

func TestInferProvider_CatalogExactMatch(t *testing.T) {
	cases := []struct {
		model    string
		provider string
	}{
		{"claude-opus-4-6", "anthropic"},
		{"claude-sonnet-4-6", "anthropic"},
		{"claude-haiku-4-5", "anthropic"},
		{"gpt-4o", "openai"},
		{"gpt-4o-mini", "openai"},
		{"o3", "openai"},
		{"o4-mini", "openai"},
		{"kimi-k2.5", "kimi"},
		{"kimi-k2", "kimi"},
		{"moonshot-v1-128k", "kimi"},
		{"moonshot-v1-32k", "kimi"},
		{"moonshot-v1-8k", "kimi"},
		{"gemini-2.0-flash-lite", "google"},
		{"gemini-2.0-flash", "google"},
		{"gemini-1.5-flash", "google"},
		{"gemini-1.5-flash-8b", "google"},
		{"llama3", "local"},
		{"mistral", "local"},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			got := providers.InferProvider(tc.model)
			if got != tc.provider {
				t.Errorf("InferProvider(%q): got %q, want %q", tc.model, got, tc.provider)
			}
		})
	}
}

func TestInferProvider_PrefixFallback(t *testing.T) {
	cases := []struct {
		model    string
		provider string
	}{
		{"claude-3-5-sonnet", "anthropic"},
		{"gpt-3.5-turbo", "openai"},
		{"o1-preview", "openai"},
		{"o4-custom", "openai"},
		{"chatgpt-4o-latest", "openai"},
		{"gemini-pro", "google"},
		{"moonshot-v2-8k", "kimi"},
		{"kimi-pro", "kimi"},
		{"llama-2-70b", "local"},
		{"mistral-7b", "local"},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			got := providers.InferProvider(tc.model)
			if got != tc.provider {
				t.Errorf("InferProvider(%q): got %q, want %q", tc.model, got, tc.provider)
			}
		})
	}
}

func TestInferProvider_CaseInsensitive(t *testing.T) {
	got := providers.InferProvider("CLAUDE-OPUS-4-6")
	if got != "anthropic" {
		t.Errorf("InferProvider(%q): got %q, want %q", "CLAUDE-OPUS-4-6", got, "anthropic")
	}
}

func TestInferProvider_Unknown(t *testing.T) {
	got := providers.InferProvider("totally-unknown-model")
	if got != "" {
		t.Errorf("InferProvider(%q): got %q, want empty string", "totally-unknown-model", got)
	}
}

// ─── Catalog ──────────────────────────────────────────────────────────────────

func TestCatalog_NotEmpty(t *testing.T) {
	if len(providers.Catalog) == 0 {
		t.Error("Catalog should not be empty")
	}
}

func TestCatalog_ContainsExpectedProviders(t *testing.T) {
	want := map[string]bool{
		"anthropic": false,
		"openai":    false,
		"kimi":      false,
		"google":    false,
		"local":     false,
	}
	for _, p := range providers.Catalog {
		if _, ok := want[p.ID]; ok {
			want[p.ID] = true
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("Catalog missing provider %q", id)
		}
	}
}

func TestCatalog_EachProviderHasModels(t *testing.T) {
	for _, p := range providers.Catalog {
		if len(p.Models) == 0 {
			t.Errorf("provider %q has no models", p.ID)
		}
	}
}

// ─── FormatProviderTable ──────────────────────────────────────────────────────

func TestFormatProviderTable_ContainsProviderNames(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("KIMI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")

	keys := providers.APIKeys{}
	out := providers.FormatProviderTable(keys)

	expectedSubstrings := []string{"Anthropic", "OpenAI", "Kimi", "Google", "Local"}
	for _, sub := range expectedSubstrings {
		if !strings.Contains(out, sub) {
			t.Errorf("FormatProviderTable output missing %q", sub)
		}
	}
}

func TestFormatProviderTable_ShowsKeyNotSetWhenEmpty(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("KIMI_API_KEY", "")

	keys := providers.APIKeys{}
	out := providers.FormatProviderTable(keys)

	if !strings.Contains(out, "not set") {
		t.Error("FormatProviderTable should contain 'not set' when no API keys are set")
	}
}

func TestFormatProviderTable_ShowsKeySetWhenPresent(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("KIMI_API_KEY", "")

	keys := providers.APIKeys{AnthropicKey: "sk-ant-test"}
	out := providers.FormatProviderTable(keys)

	if !strings.Contains(out, "key: set") {
		t.Errorf("FormatProviderTable should contain 'key: set' for Anthropic when key is set; got:\n%s", out)
	}
}

func TestFormatProviderTable_ContainsModelIDs(t *testing.T) {
	keys := providers.APIKeys{}
	out := providers.FormatProviderTable(keys)

	modelIDs := []string{"claude-opus-4-6", "gpt-4o", "kimi-k2.5", "gemini-2.0-flash", "llama3"}
	for _, id := range modelIDs {
		if !strings.Contains(out, id) {
			t.Errorf("FormatProviderTable output missing model %q", id)
		}
	}
}

func TestFormatProviderTable_LocalHasNoKeyStatus(t *testing.T) {
	keys := providers.APIKeys{}
	out := providers.FormatProviderTable(keys)

	// Local provider has no EnvKey — its header line should not contain "[key:"
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Local") && strings.Contains(line, "[key:") {
			t.Errorf("Local provider line should not contain '[key:' status, got: %q", line)
		}
	}
}

// ─── Chat ─────────────────────────────────────────────────────────────────────

func TestChat_UnsupportedProvider(t *testing.T) {
	// Error path for an unsupported provider — no network call needed.
	req := providers.DirectChatRequest{
		Provider: "totally-unknown",
		Model:    "some-model",
		Messages: []providers.DirectMessage{{Role: "user", Content: "hello"}},
		APIKey:   "fake",
	}

	_, err := providers.Chat(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for unsupported provider, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("error message should mention 'unsupported provider'; got: %v", err)
	}
}

func TestChat_Google_CancelledContext(t *testing.T) {
	// Pre-cancelled context exercises the chatGoogle error path.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := providers.DirectChatRequest{
		Provider:  "google",
		Model:     "gemini-2.0-flash",
		Messages:  []providers.DirectMessage{{Role: "user", Content: "ping"}},
		MaxTokens: 16,
		APIKey:    "google-fake",
	}

	_, err := providers.Chat(ctx, req)
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}
}

func TestChat_Anthropic_CancelledContext(t *testing.T) {
	// A pre-cancelled context causes the HTTP client to error immediately,
	// exercising the chatAnthropic request-construction and Do() error paths.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call

	req := providers.DirectChatRequest{
		Provider:  "anthropic",
		Model:     "claude-sonnet-4-6",
		Messages:  []providers.DirectMessage{{Role: "user", Content: "ping"}},
		MaxTokens: 16,
		APIKey:    "sk-ant-fake",
	}

	_, err := providers.Chat(ctx, req)
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}
}

func TestChat_OpenAI_CancelledContext(t *testing.T) {
	// Pre-cancelled context exercises the chatOpenAICompatible error path for openai.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := providers.DirectChatRequest{
		Provider:  "openai",
		Model:     "gpt-4o",
		Messages:  []providers.DirectMessage{{Role: "user", Content: "ping"}},
		MaxTokens: 16,
		APIKey:    "sk-openai-fake",
	}

	_, err := providers.Chat(ctx, req)
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}
}

func TestChat_Kimi_CancelledContext(t *testing.T) {
	// Pre-cancelled context exercises the chatOpenAICompatible error path for kimi.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := providers.DirectChatRequest{
		Provider:  "kimi",
		Model:     "kimi-k2.5",
		Messages:  []providers.DirectMessage{{Role: "user", Content: "ping"}},
		MaxTokens: 16,
		APIKey:    "kimi-fake",
	}

	_, err := providers.Chat(ctx, req)
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}
}

func TestChat_Anthropic_DefaultMaxTokens(t *testing.T) {
	// MaxTokens = 0 triggers the default-to-1024 path inside chatAnthropic.
	// Context is cancelled so no real network call happens.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := providers.DirectChatRequest{
		Provider:  "anthropic",
		Model:     "claude-haiku-4-5",
		Messages:  []providers.DirectMessage{{Role: "user", Content: "hi"}},
		MaxTokens: 0, // triggers default path
		APIKey:    "sk-ant-fake",
	}

	_, err := providers.Chat(ctx, req)
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}
}
