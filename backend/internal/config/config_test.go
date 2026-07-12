package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validAgent returns an AgentConfig that passes validation, so each test can
// tweak a single field to assert the failure it cares about.
func validAgent() AgentConfig {
	return AgentConfig{
		ArxivCategory:         "cs.AI",
		ArxivBaseURL:          "https://export.arxiv.org/api/query",
		FetchLimit:            20,
		DisplayLimit:          5,
		UserAgent:             "arxiv-explainer-agent/1.0",
		RequestTimeoutSec:     10,
		MinRequestIntervalSec: 3,
		MaxRetries:            3,
		ArxivHTMLBaseURL:      "https://arxiv.org/html",
		MaxContentBytes:       52428800,
	}
}

// validLLM returns an LLMConfig that passes validation, so each test can tweak a
// single field to assert the failure it cares about.
func validLLM() LLMConfig {
	return LLMConfig{
		Provider:          "anthropic",
		Model:             "claude-sonnet-4-6",
		APIKey:            "test-key",
		MaxTokens:         4096,
		Temperature:       0.3,
		RequestTimeoutSec: 120,
	}
}

func TestAgentConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*AgentConfig)
		wantErr string // substring expected in the error; "" means expect success
	}{
		{"valid", func(*AgentConfig) {}, ""},
		{"empty category", func(a *AgentConfig) { a.ArxivCategory = "" }, "arxiv_category"},
		{"empty base url", func(a *AgentConfig) { a.ArxivBaseURL = "" }, "arxiv_base_url"},
		{"empty user agent", func(a *AgentConfig) { a.UserAgent = "" }, "user_agent"},
		{"zero fetch limit", func(a *AgentConfig) { a.FetchLimit = 0 }, "fetch_limit"},
		{"zero display limit", func(a *AgentConfig) { a.DisplayLimit = 0 }, "display_limit"},
		{"display exceeds fetch", func(a *AgentConfig) { a.DisplayLimit = 21 }, "must not exceed"},
		{"zero timeout", func(a *AgentConfig) { a.RequestTimeoutSec = 0 }, "request_timeout_seconds"},
		{"negative interval", func(a *AgentConfig) { a.MinRequestIntervalSec = -1 }, "min_request_interval_seconds"},
		{"negative retries", func(a *AgentConfig) { a.MaxRetries = -1 }, "max_retries"},
		{"empty html base url", func(a *AgentConfig) { a.ArxivHTMLBaseURL = "" }, "arxiv_html_base_url"},
		{"zero max content bytes", func(a *AgentConfig) { a.MaxContentBytes = 0 }, "max_content_bytes"},
		{"negative max content bytes", func(a *AgentConfig) { a.MaxContentBytes = -1 }, "max_content_bytes"},
		{"negative max review iterations", func(a *AgentConfig) { a.MaxReviewIterations = -1 }, "max_review_iterations"},
		{"zero max review iterations ok", func(a *AgentConfig) { a.MaxReviewIterations = 0 }, ""},
		{"positive max review iterations ok", func(a *AgentConfig) { a.MaxReviewIterations = 2 }, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := validAgent()
			tt.mutate(&a)
			err := a.validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

// validConfig assembles a Config that passes validate(), letting each LLM test
// tweak one field. Paths are absolute so the path checks pass.
func validConfig() *Config {
	return &Config{
		LLM:   validLLM(),
		Agent: validAgent(),
		Paths: PathsConfig{ObsidianVault: "/tmp/vault", LogFile: "/tmp/log.json"},
	}
}

// TestLoadEnvOverrides verifies the .env-style overrides in Load() win over the
// values parsed from config.yaml. base_url is the newest override, so it gets a
// dedicated assertion alongside provider/model. t.Setenv restores env + isolates
// the test; the temp config.yaml keeps base_url unset so we prove the override,
// not the YAML value, is what lands.
func TestLoadEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(minimalYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LLM_API_KEY", "test-key")
	t.Setenv("LLM_PROVIDER", "openai")
	t.Setenv("LLM_MODEL", "gpt-4o-mini")
	t.Setenv("LLM_BASE_URL", "https://proxy.example/v1")
	t.Setenv("LLM_MAX_TOKENS", "2048")             // int override
	t.Setenv("AGENT_FETCH_LIMIT", "42")            // int override in another section
	t.Setenv("AGENT_MAX_CONTENT_BYTES", "1048576") // int64 override

	cfg, err := Load(yamlPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLM.Provider != "openai" {
		t.Errorf("provider = %q, want openai", cfg.LLM.Provider)
	}
	if cfg.LLM.Model != "gpt-4o-mini" {
		t.Errorf("model = %q, want gpt-4o-mini", cfg.LLM.Model)
	}
	if cfg.LLM.BaseURL != "https://proxy.example/v1" {
		t.Errorf("base_url = %q, want the LLM_BASE_URL override", cfg.LLM.BaseURL)
	}
	if cfg.LLM.MaxTokens != 2048 {
		t.Errorf("max_tokens = %d, want 2048", cfg.LLM.MaxTokens)
	}
	if cfg.Agent.FetchLimit != 42 {
		t.Errorf("fetch_limit = %d, want 42", cfg.Agent.FetchLimit)
	}
	if cfg.Agent.MaxContentBytes != 1048576 {
		t.Errorf("max_content_bytes = %d, want 1048576", cfg.Agent.MaxContentBytes)
	}
}

// TestLoadEnvOverrideMalformed proves a non-numeric numeric override is a hard
// startup error (fail-fast) instead of silently falling back to the YAML value.
func TestLoadEnvOverrideMalformed(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(minimalYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LLM_API_KEY", "test-key")
	t.Setenv("LLM_MAX_TOKENS", "not-a-number")

	_, err := Load(yamlPath)
	if err == nil || !strings.Contains(err.Error(), "LLM_MAX_TOKENS") {
		t.Fatalf("expected LLM_MAX_TOKENS parse error, got %v", err)
	}
}

// minimalYAML is a valid config.yaml body shared by the Load tests.
const minimalYAML = `llm:
  provider: anthropic
  model: claude-sonnet-4-6
  max_tokens: 4096
  temperature: 0.3
  request_timeout_seconds: 120
  base_url: ""
paths:
  obsidian_vault: /tmp/vault
  log_file: /tmp/log.json
agent:
  arxiv_category: cs.AI
  arxiv_base_url: https://export.arxiv.org/api/query
  fetch_limit: 20
  display_limit: 5
  user_agent: arxiv-explainer-agent/1.0
  request_timeout_seconds: 10
  min_request_interval_seconds: 3
  max_retries: 3
  arxiv_html_base_url: https://arxiv.org/html
  max_content_bytes: 52428800
`

func TestLLMConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*LLMConfig)
		wantErr string
	}{
		{"valid", func(*LLMConfig) {}, ""},
		{"empty api key", func(l *LLMConfig) { l.APIKey = "" }, "LLM_API_KEY"},
		{"invalid provider", func(l *LLMConfig) { l.Provider = "bogus" }, "llm.provider"},
		{"empty model", func(l *LLMConfig) { l.Model = "" }, "llm.model"},
		{"zero max tokens", func(l *LLMConfig) { l.MaxTokens = 0 }, "llm.max_tokens"},
		{"negative temperature", func(l *LLMConfig) { l.Temperature = -0.1 }, "llm.temperature"},
		{"too-high temperature", func(l *LLMConfig) { l.Temperature = 2.1 }, "llm.temperature"},
		{"zero llm timeout", func(l *LLMConfig) { l.RequestTimeoutSec = 0 }, "llm.request_timeout_seconds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(&cfg.LLM)
			err := cfg.validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}
