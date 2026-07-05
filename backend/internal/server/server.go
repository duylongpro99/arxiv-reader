package server

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/orchestrator"
)

// addr binds loopback ONLY (F6) — never 0.0.0.0.
const addr = "127.0.0.1:8080"

// Handler builds the fully-wired, CORS-wrapped HTTP handler. Extracted from Run
// so integration tests can drive the real routes via httptest without binding a
// socket. It returns an error because orchestrator.New can fail (invalid LLM
// provider) — a bad provider must stop startup, not surface at first request.
func Handler(cfg *config.Config) (http.Handler, error) {
	orch, err := orchestrator.New(cfg)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler) // method-based routing (Go 1.22+)
	mux.HandleFunc("POST /discover", orch.HandleDiscover)
	mux.HandleFunc("POST /process", orch.HandleProcess)
	mux.HandleFunc("POST /retry/{sessionId}", orch.HandleRetry)
	mux.HandleFunc("GET /status/{sessionId}", orch.HandleStatus)
	mux.HandleFunc("GET /result/{sessionId}", orch.HandleResult)

	return corsMiddleware(mux), nil
}

// Run binds the loopback socket, then serves the Handler.
// cfg is threaded in from Phase 2 onward so discovery handlers can read the
// agent/paths config.
func Run(cfg *config.Config) error {
	handler, err := Handler(cfg)
	if err != nil {
		return fmt.Errorf("cannot build handler: %w", err)
	}

	// Bind FIRST so "server listening" is only logged once the socket is up
	// (RT3): if the port is taken, Listen fails here and we never lie in logs.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot bind %s: %w", addr, err)
	}
	slog.Info("server listening", "addr", addr)
	return http.Serve(ln, handler)
}

// corsMiddleware restricts cross-origin access to the local Next.js dev origin
// and short-circuits preflight OPTIONS. Policy is intentionally narrow (F6).
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
