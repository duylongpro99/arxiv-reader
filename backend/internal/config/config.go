package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// Config is the fully-resolved runtime configuration for Phase 1.
type Config struct {
	LLM   LLMConfig   `yaml:"llm"`
	Paths PathsConfig `yaml:"paths"`
}

type LLMConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	APIKey   string `yaml:"-"` // from .env ONLY — never parsed from config.yaml
}

type PathsConfig struct {
	ObsidianVault string `yaml:"obsidian_vault"`
	LogFile       string `yaml:"log_file"`
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
	if !filepath.IsAbs(c.Paths.ObsidianVault) {
		return fmt.Errorf("paths.obsidian_vault must be an absolute path, got %q.\n  → Set paths.obsidian_vault in config.yaml (or OBSIDIAN_VAULT_PATH in .env)", c.Paths.ObsidianVault)
	}
	if !filepath.IsAbs(c.Paths.LogFile) {
		return fmt.Errorf("paths.log_file must be an absolute path, got %q.\n  → Set paths.log_file in config.yaml", c.Paths.LogFile)
	}
	return nil
}
