package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// Config is the fully-resolved runtime configuration.
type Config struct {
	LLM     LLMConfig     `yaml:"llm"`
	Paths   PathsConfig   `yaml:"paths"`
	Agent   AgentConfig   `yaml:"agent"`
	Tracing TracingConfig `yaml:"tracing"`
	// DatabaseURL is the Postgres DSN for durable run-timeline history. Read from
	// .env ONLY (like the API key; yaml:"-") so a secret never lands in the
	// committed config.yaml. Empty is VALID: tracing then degrades to in-memory
	// only (live SSE still works; history/reload disabled) — never fatal.
	DatabaseURL string `yaml:"-"`
}

// TracingConfig holds the Phase 7 run-timeline knobs. Enabled is the master
// switch for the Recorder; FullPayloads opts into storing full prompts/
// responses/markdown (off by default — summaries + previews only); BufferSize
// is the per-run in-memory ring capacity feeding SSE replay.
type TracingConfig struct {
	Enabled      bool `yaml:"enabled"`
	FullPayloads bool `yaml:"full_payloads"`
	BufferSize   int  `yaml:"buffer_size"` // > 0 when Enabled
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
	// ResourcesDir holds the declarative resource engine's *.yaml declarations
	// (default ./resources; env RESOURCES_DIR). The loader reads it at startup.
	ResourcesDir string `yaml:"resources_dir"`
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
	// Phase 5 reviewer loop: max critic→revision rounds per paper. 0 disables the
	// reviewer entirely (reproduces Phase-4 behaviour at zero reviewer cost).
	MaxReviewIterations int `yaml:"max_review_iterations"` // >= 0; default 2
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
	//    a missing .env is not fatal (real exported env vars still apply). Every
	//    tunable is overridable so a deployment can retune without a rebuild.
	_ = godotenv.Load()
	if err := cfg.applyEnvOverrides(); err != nil {
		return nil, err
	}

	// 3. expand ~ BEFORE the absolute-path check (FIX #1): PRD defaults use ~,
	//    and Go's filepath.IsAbs treats "~/..." as relative.
	cfg.Paths.ObsidianVault = expandHome(cfg.Paths.ObsidianVault)
	cfg.Paths.LogFile = expandHome(cfg.Paths.LogFile)
	// Default the resources dir so an existing config.yaml without the key boots.
	if cfg.Paths.ResourcesDir == "" {
		cfg.Paths.ResourcesDir = "./resources"
	}

	// 4. fail-fast validation
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// applyEnvOverrides layers environment values on top of the config.yaml
// defaults so every tunable can change per deployment without a rebuild. Names
// follow SECTION_FIELD in screaming snake case (e.g. AGENT_FETCH_LIMIT); the
// pre-existing LLM_*/OBSIDIAN_VAULT_PATH names are kept for compatibility. The
// API key is override-only (never read from YAML). Malformed numeric values are
// collected and returned as one fail-fast error rather than silently ignored.
func (c *Config) applyEnvOverrides() error {
	c.LLM.APIKey = os.Getenv("LLM_API_KEY") // required; validated below. Override-only.

	var errs []string
	// LLM
	envStr("LLM_PROVIDER", &c.LLM.Provider)
	envStr("LLM_MODEL", &c.LLM.Model)
	envStr("LLM_BASE_URL", &c.LLM.BaseURL) // custom OpenAI-compatible endpoint/proxy
	envInt("LLM_MAX_TOKENS", &c.LLM.MaxTokens, &errs)
	envFloat32("LLM_TEMPERATURE", &c.LLM.Temperature, &errs)
	envInt("LLM_REQUEST_TIMEOUT_SECONDS", &c.LLM.RequestTimeoutSec, &errs)
	// Paths
	envStr("OBSIDIAN_VAULT_PATH", &c.Paths.ObsidianVault)
	envStr("LOG_FILE", &c.Paths.LogFile)
	envStr("RESOURCES_DIR", &c.Paths.ResourcesDir)
	// Agent
	envStr("AGENT_ARXIV_CATEGORY", &c.Agent.ArxivCategory)
	envStr("AGENT_ARXIV_BASE_URL", &c.Agent.ArxivBaseURL)
	envInt("AGENT_FETCH_LIMIT", &c.Agent.FetchLimit, &errs)
	envInt("AGENT_DISPLAY_LIMIT", &c.Agent.DisplayLimit, &errs)
	envStr("AGENT_USER_AGENT", &c.Agent.UserAgent)
	envInt("AGENT_REQUEST_TIMEOUT_SECONDS", &c.Agent.RequestTimeoutSec, &errs)
	envInt("AGENT_MIN_REQUEST_INTERVAL_SECONDS", &c.Agent.MinRequestIntervalSec, &errs)
	envInt("AGENT_MAX_RETRIES", &c.Agent.MaxRetries, &errs)
	envStr("AGENT_ARXIV_HTML_BASE_URL", &c.Agent.ArxivHTMLBaseURL)
	envInt64("AGENT_MAX_CONTENT_BYTES", &c.Agent.MaxContentBytes, &errs)
	envInt("AGENT_MAX_REVIEW_ITERATIONS", &c.Agent.MaxReviewIterations, &errs)
	// Tracing + DB. DATABASE_URL is override-only (never from YAML), mirroring the
	// API key: a DSN can carry a password, so it stays out of the committed file.
	envStr("DATABASE_URL", &c.DatabaseURL)
	envBool("TRACING_ENABLED", &c.Tracing.Enabled, &errs)
	envBool("TRACING_FULL_PAYLOADS", &c.Tracing.FullPayloads, &errs)
	envInt("TRACING_BUFFER_SIZE", &c.Tracing.BufferSize, &errs)

	if len(errs) > 0 {
		return fmt.Errorf("invalid environment override(s):\n  → %s", strings.Join(errs, "\n  → "))
	}
	return nil
}

// envStr overwrites *dst when key is set and non-empty. Empty/unset is a no-op,
// so a blank env var never clobbers a meaningful YAML default.
func envStr(key string, dst *string) {
	if v := os.Getenv(key); v != "" {
		*dst = v
	}
}

// envInt / envInt64 / envFloat32 parse a numeric override; a malformed value
// appends a keyed message to *errs (fail-fast) instead of falling back to the
// YAML default, which would silently mask a deployment typo.
func envInt(key string, dst *int, errs *[]string) {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			*errs = append(*errs, fmt.Sprintf("%s=%q is not a valid integer", key, v))
			return
		}
		*dst = n
	}
}

func envInt64(key string, dst *int64, errs *[]string) {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			*errs = append(*errs, fmt.Sprintf("%s=%q is not a valid integer", key, v))
			return
		}
		*dst = n
	}
}

func envFloat32(key string, dst *float32, errs *[]string) {
	if v := os.Getenv(key); v != "" {
		f, err := strconv.ParseFloat(v, 32)
		if err != nil {
			*errs = append(*errs, fmt.Sprintf("%s=%q is not a valid number", key, v))
			return
		}
		*dst = float32(f)
	}
}

// envBool parses a boolean override (1/t/true/0/f/false, case-insensitive per
// strconv.ParseBool); a malformed value fails fast like the numeric parsers
// rather than silently defaulting.
func envBool(key string, dst *bool, errs *[]string) {
	if v := os.Getenv(key); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			*errs = append(*errs, fmt.Sprintf("%s=%q is not a valid boolean (use true/false)", key, v))
			return
		}
		*dst = b
	}
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
	// Tracing: buffer_size must be positive when enabled (it sizes the per-run
	// ring buffer). DatabaseURL is intentionally NOT required — an empty DSN is
	// the documented in-memory-only degrade path, never a validation error.
	if c.Tracing.Enabled && c.Tracing.BufferSize <= 0 {
		return fmt.Errorf("tracing.buffer_size must be > 0 when tracing is enabled, got %d.\n  → Set tracing.buffer_size in config.yaml", c.Tracing.BufferSize)
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
	// The configured category is the DEFAULT selection (consumed by
	// resources/arxiv.yaml via ${AGENT_ARXIV_CATEGORY}); the resource loader
	// validates it is in the catalog (default-in-catalog check), so no cs.*
	// whitelist lives here anymore — the catalog is the single source of truth.
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
	// 0 is valid: it disables the reviewer. Only a negative value is rejected.
	if a.MaxReviewIterations < 0 {
		return fmt.Errorf("agent.max_review_iterations must be >= 0, got %d.\n  → Set agent.max_review_iterations in config.yaml (0 disables the reviewer)", a.MaxReviewIterations)
	}
	return nil
}

// Resolve is the ${...} lookup the resource loader uses to expand config
// references in resources/*.yaml. It reads the MERGED *Config field map first
// (so a config.yaml-only value with no matching env var still resolves — F11),
// then falls back to os.Getenv, and finally errors on an unknown key (fail-fast,
// key-free message). Only whitelisted keys resolve — an unrelated env var cannot
// be spliced into a declaration.
func (c *Config) Resolve(key string) (string, error) {
	merged := map[string]string{
		"AGENT_ARXIV_CATEGORY":               c.Agent.ArxivCategory,
		"AGENT_ARXIV_BASE_URL":               c.Agent.ArxivBaseURL,
		"AGENT_FETCH_LIMIT":                  strconv.Itoa(c.Agent.FetchLimit),
		"AGENT_USER_AGENT":                   c.Agent.UserAgent,
		"AGENT_REQUEST_TIMEOUT_SECONDS":      strconv.Itoa(c.Agent.RequestTimeoutSec),
		"AGENT_MIN_REQUEST_INTERVAL_SECONDS": strconv.Itoa(c.Agent.MinRequestIntervalSec),
		"AGENT_MAX_RETRIES":                  strconv.Itoa(c.Agent.MaxRetries),
		"AGENT_ARXIV_HTML_BASE_URL":          c.Agent.ArxivHTMLBaseURL,
		"AGENT_MAX_CONTENT_BYTES":            strconv.FormatInt(c.Agent.MaxContentBytes, 10),
	}
	if v, ok := merged[key]; ok && v != "" {
		return v, nil
	}
	if v := os.Getenv(key); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("unresolved config reference %q in a resource declaration", key)
}
