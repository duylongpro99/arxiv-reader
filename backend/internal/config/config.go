package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// Config is the fully-resolved runtime configuration.
type Config struct {
	LLM   LLMConfig   `yaml:"llm"`
	Paths PathsConfig `yaml:"paths"`
	Agent AgentConfig `yaml:"agent"`
}

type LLMConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	APIKey   string `yaml:"-"` // from .env ONLY — never parsed from config.yaml
	// LLM knobs consumed by Phase 3/4 provider clients. LLM calls are slow, so
	// RequestTimeoutSec is a separate, larger budget than the arXiv agent timeout.
	MaxTokens         int     `yaml:"max_tokens"`              // > 0
	Temperature       float32 `yaml:"temperature"`             // 0..2
	RequestTimeoutSec int     `yaml:"request_timeout_seconds"` // > 0
	BaseURL           string  `yaml:"base_url"`                // optional; "" = provider default
}

type PathsConfig struct {
	ObsidianVault string `yaml:"obsidian_vault"`
	LogFile       string `yaml:"log_file"`
}

// AgentConfig holds the discovery-pipeline knobs introduced in Phase 2.
// arXiv params live here (not hardcoded) so tuning stays out of code and tests
// can point ArxivBaseURL at an httptest.Server instead of the live API.
type AgentConfig struct {
	ArxivCategory         string `yaml:"arxiv_category"`
	ArxivBaseURL          string `yaml:"arxiv_base_url"`
	FetchLimit            int    `yaml:"fetch_limit"`   // papers pulled from arXiv (buffer)
	DisplayLimit          int    `yaml:"display_limit"` // candidates surfaced to the user
	UserAgent             string `yaml:"user_agent"`
	RequestTimeoutSec     int    `yaml:"request_timeout_seconds"`
	MinRequestIntervalSec int    `yaml:"min_request_interval_seconds"`
	MaxRetries            int    `yaml:"max_retries"`
	// Phase 3 HTML extraction: base URL for arXiv's LaTeXML HTML rendering
	// (configurable so tests can point at an httptest.Server, like ArxivBaseURL),
	// and a byte cap feeding io.LimitReader as the OOM guard on fetched bodies.
	ArxivHTMLBaseURL string `yaml:"arxiv_html_base_url"` // default https://arxiv.org/html
	MaxContentBytes  int64  `yaml:"max_content_bytes"`   // io.LimitReader cap; > 0
}

// validProviders is the whitelist enforced by validate().
var validProviders = map[string]bool{"anthropic": true, "openai": true, "gemini": true}

// Load reads config.yaml (defaults), applies .env overrides, expands a leading
// ~ on paths, then validates. Returns an error (caller decides fatal handling);
// error messages are key-free by construction so they are safe to log.
func Load(yamlPath string) (*Config, error) {
	// 1. defaults from config.yaml
	raw, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", yamlPath, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("cannot parse %s: %w", yamlPath, err)
	}

	// 2. .env overrides. godotenv.Load loads .env into process env if present;
	//    a missing .env is not fatal (real exported env vars still apply).
	_ = godotenv.Load()
	cfg.LLM.APIKey = os.Getenv("LLM_API_KEY") // required; validated below
	if v := os.Getenv("LLM_PROVIDER"); v != "" {
		cfg.LLM.Provider = v
	}
	if v := os.Getenv("LLM_MODEL"); v != "" {
		cfg.LLM.Model = v
	}
	if v := os.Getenv("OBSIDIAN_VAULT_PATH"); v != "" {
		cfg.Paths.ObsidianVault = v
	}

	// 3. expand ~ BEFORE the absolute-path check (FIX #1): PRD defaults use ~,
	//    and Go's filepath.IsAbs treats "~/..." as relative.
	cfg.Paths.ObsidianVault = expandHome(cfg.Paths.ObsidianVault)
	cfg.Paths.LogFile = expandHome(cfg.Paths.LogFile)

	// 4. fail-fast validation
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// expandHome replaces a leading ~ (only "~" or "~/…") with $HOME, mirroring
// shell behaviour. On HOME lookup failure the value is returned unchanged so
// the absolute-path check reports it.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}

// validate enforces F3. Each message names the field and states the fix; none
// echoes the API key value.
func (c *Config) validate() error {
	if c.LLM.APIKey == "" {
		return fmt.Errorf("LLM_API_KEY is required but not set.\n  → Add LLM_API_KEY=your_key_here to your .env file")
	}
	if !validProviders[c.LLM.Provider] {
		return fmt.Errorf("llm.provider %q is not valid.\n  → Must be one of: anthropic, openai, gemini", c.LLM.Provider)
	}
	if c.LLM.Model == "" {
		return fmt.Errorf("llm.model is required but not set.\n  → Set llm.model in config.yaml (or LLM_MODEL in .env)")
	}
	if c.LLM.MaxTokens <= 0 {
		return fmt.Errorf("llm.max_tokens must be > 0, got %d.\n  → Set llm.max_tokens in config.yaml", c.LLM.MaxTokens)
	}
	if c.LLM.Temperature < 0 || c.LLM.Temperature > 2 {
		return fmt.Errorf("llm.temperature must be between 0 and 2, got %v.\n  → Set llm.temperature in config.yaml", c.LLM.Temperature)
	}
	if c.LLM.RequestTimeoutSec <= 0 {
		return fmt.Errorf("llm.request_timeout_seconds must be > 0, got %d.\n  → Set llm.request_timeout_seconds in config.yaml", c.LLM.RequestTimeoutSec)
	}
	if !filepath.IsAbs(c.Paths.ObsidianVault) {
		return fmt.Errorf("paths.obsidian_vault must be an absolute path, got %q.\n  → Set paths.obsidian_vault in config.yaml (or OBSIDIAN_VAULT_PATH in .env)", c.Paths.ObsidianVault)
	}
	if !filepath.IsAbs(c.Paths.LogFile) {
		return fmt.Errorf("paths.log_file must be an absolute path, got %q.\n  → Set paths.log_file in config.yaml", c.Paths.LogFile)
	}
	return c.Agent.validate()
}

// validate enforces the Phase 2 agent section. A missing agent block leaves the
// int fields at their zero value, so the >0 checks double as presence checks —
// preventing a silent fetch_limit:0 from surfacing zero candidates downstream.
func (a *AgentConfig) validate() error {
	if a.ArxivCategory == "" {
		return fmt.Errorf("agent.arxiv_category is required but not set.\n  → Set agent.arxiv_category (e.g. cs.AI) in config.yaml")
	}
	if a.ArxivBaseURL == "" {
		return fmt.Errorf("agent.arxiv_base_url is required but not set.\n  → Set agent.arxiv_base_url in config.yaml")
	}
	if a.UserAgent == "" {
		return fmt.Errorf("agent.user_agent is required but not set.\n  → Set agent.user_agent in config.yaml")
	}
	if a.FetchLimit <= 0 {
		return fmt.Errorf("agent.fetch_limit must be > 0, got %d.\n  → Set agent.fetch_limit in config.yaml", a.FetchLimit)
	}
	if a.DisplayLimit <= 0 {
		return fmt.Errorf("agent.display_limit must be > 0, got %d.\n  → Set agent.display_limit in config.yaml", a.DisplayLimit)
	}
	if a.DisplayLimit > a.FetchLimit {
		return fmt.Errorf("agent.display_limit (%d) must not exceed agent.fetch_limit (%d).\n  → Lower display_limit or raise fetch_limit in config.yaml", a.DisplayLimit, a.FetchLimit)
	}
	if a.RequestTimeoutSec <= 0 {
		return fmt.Errorf("agent.request_timeout_seconds must be > 0, got %d.\n  → Set agent.request_timeout_seconds in config.yaml", a.RequestTimeoutSec)
	}
	if a.MinRequestIntervalSec < 0 {
		return fmt.Errorf("agent.min_request_interval_seconds must be >= 0, got %d.\n  → Set agent.min_request_interval_seconds in config.yaml", a.MinRequestIntervalSec)
	}
	if a.MaxRetries < 0 {
		return fmt.Errorf("agent.max_retries must be >= 0, got %d.\n  → Set agent.max_retries in config.yaml", a.MaxRetries)
	}
	if a.ArxivHTMLBaseURL == "" {
		return fmt.Errorf("agent.arxiv_html_base_url is required but not set.\n  → Set agent.arxiv_html_base_url (e.g. https://arxiv.org/html) in config.yaml")
	}
	if a.MaxContentBytes <= 0 {
		return fmt.Errorf("agent.max_content_bytes must be > 0, got %d.\n  → Set agent.max_content_bytes in config.yaml", a.MaxContentBytes)
	}
	return nil
}
