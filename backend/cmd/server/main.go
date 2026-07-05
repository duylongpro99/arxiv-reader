package main

import (
	"log/slog"
	"os"

	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/server"
)

func main() {
	// Structured JSON logging established here for all phases.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	// cfg loaded relative to cwd; Makefile/.air.toml guarantee cwd=repo root.
	cfg, err := config.Load("config.yaml")
	if err != nil {
		// error is key-free by construction — safe to log.
		slog.Error("FATAL config error", "error", err.Error())
		os.Exit(1)
	}
	// Log provider+model+vault path only. NEVER log cfg.LLM.APIKey.
	slog.Info("config loaded", "provider", cfg.LLM.Provider, "model", cfg.LLM.Model, "vault_path", cfg.Paths.ObsidianVault)

	if err := server.Run(cfg); err != nil {
		slog.Error("server error", "error", err.Error())
		os.Exit(1)
	}
}
