.PHONY: dev check-tools install

check-tools:
	@command -v go  >/dev/null 2>&1 || { echo "ERROR: 'go' not found. Install Go 1.23+ from https://go.dev/dl/ and retry."; exit 1; }
	@command -v air >/dev/null 2>&1 || { echo "ERROR: 'air' not found. Run: go install github.com/air-verse/air@latest  (then add \$$(go env GOPATH)/bin to PATH)"; exit 1; }
	@command -v npm >/dev/null 2>&1 || { echo "ERROR: 'npm' not found. Install Node.js 20+ from https://nodejs.org"; exit 1; }

install:
	@[ -d frontend/node_modules ] || { echo "Installing frontend deps (npm ci)..."; cd frontend && npm ci; }

# make dev: frontend :3000 + backend :8080 concurrently, live-reload, single teardown.
# One shell (\ continuations) so `kill 0` covers both bg jobs. Poll loop guarantees
# a single-service crash also tears down the other (bash 3.2 lacks `wait -n`).
dev: check-tools install
	@echo "Starting frontend :3000 and backend :8080 (Ctrl-C to stop both)..."
	@trap 'kill 0' EXIT INT TERM; \
		air -c backend/.air.toml & be=$$!; \
		( cd frontend && npm run dev ) & fe=$$!; \
		while kill -0 $$be 2>/dev/null && kill -0 $$fe 2>/dev/null; do sleep 1; done; \
		echo "A service exited — shutting down the other..."; \
		kill 0
