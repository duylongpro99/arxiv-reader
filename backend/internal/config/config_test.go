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
