package config

import (
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
