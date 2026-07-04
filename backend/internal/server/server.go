package server

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
)

// addr binds loopback ONLY (F6) — never 0.0.0.0.
const addr = "127.0.0.1:8080"

// Run registers routes, binds the loopback socket, then serves with CORS.
// Takes no config yet (YAGNI); Phase 2+ adds a param when a handler needs it.
func Run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler) // method-based routing (Go 1.22+)

	// Bind FIRST so "server listening" is only logged once the socket is up
	// (RT3): if the port is taken, Listen fails here and we never lie in logs.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot bind %s: %w", addr, err)
	}
	slog.Info("server listening", "addr", addr)
	return http.Serve(ln, corsMiddleware(mux))
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
