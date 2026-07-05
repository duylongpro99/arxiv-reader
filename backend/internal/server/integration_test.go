package server_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/models"
	"github.com/maritime-ds/arxiv-reader/internal/orchestrator"
	"github.com/maritime-ds/arxiv-reader/internal/server"
)

// buildFeed returns an arXiv Atom feed with n entries, IDs 2401.0001..000n.
func buildFeed(n int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><feed xmlns="http://www.w3.org/2005/Atom">`)
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("2401.%04d", i)
		fmt.Fprintf(&b, `<entry>
			<id>http://arxiv.org/abs/%sv1</id>
			<title>Paper %d</title>
			<summary>Abstract for paper %d.</summary>
			<published>2024-01-%02dT00:00:00Z</published>
			<author><name>Author %d</name></author>
			<link href="http://arxiv.org/pdf/%sv1" rel="related" type="application/pdf"/>
		</entry>`, id, i, i, i, i, id)
	}
	b.WriteString(`</feed>`)
	return b.String()
}

// latexmlSample is a minimal LaTeXML-style body used by the paper-HTML server.
const latexmlSample = `<!DOCTYPE html><html><body>
<h1>Extracted Paper Title</h1><p>The core contribution.</p></body></html>`

// cannedNote is the fake LLM's reply: a well-formed 9-section explainer. Used to
// drive the Phase 4 pipeline deterministically (no API cost, no network).
const cannedNote = `# Extracted Paper Title — Explained

## Problem Statement
Sequential models are slow.

## Core Idea
Use attention instead of recurrence.

## Methodology
Stacked attention blocks.

## Key Findings
Faster training, better results.

## Limitations
Quadratic memory.

## Why It Matters
Foundation of modern LLMs.

## Analogies & Intuition
Everyone hears everyone at once.

## Glossary
**Attention** — weighting inputs by relevance.

## Follow-Up Papers
- BERT (https://arxiv.org/abs/1810.04805)
- Suggested: GPT-3
`

// testEnv bundles the wired app plus the on-disk paths tests assert against.
type testEnv struct {
	app      *httptest.Server
	logPath  string
	vaultDir string
}

// fakeLLMServer returns an httptest server that answers the Anthropic Messages
// API with the canned note. The anthropic client honors cfg.LLM.BaseURL, so
// pointing it here exercises the real client/pipeline with a deterministic reply.
func fakeLLMServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		body, _ := json.Marshal(map[string]any{
			"id": "msg_test", "type": "message", "role": "assistant",
			"model":       "test-model",
			"content":     []map[string]any{{"type": "text", "text": cannedNote}},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 1200, "output_tokens": 800},
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// setup wires a real server.Handler against a fake arXiv (Atom API), a fake LLM,
// a temp log file, and a temp vault. The paper-HTML server always serves 200.
func setup(t *testing.T, feed string, logContent string) testEnv {
	return setupWith(t, feed, logContent, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(latexmlSample))
	})
}

// setupWith is setup with a caller-controlled paper-HTML handler (e.g. to force
// a 404 for the re-pick path).
func setupWith(t *testing.T, feed, logContent string, htmlHandler http.HandlerFunc) testEnv {
	t.Helper()

	arxiv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
	t.Cleanup(arxiv.Close)

	htmlSrv := httptest.NewServer(htmlHandler)
	t.Cleanup(htmlSrv.Close)

	llmSrv := fakeLLMServer(t)

	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "processed.json")
	vaultDir := filepath.Join(tmp, "vault")
	if logContent != "" {
		if err := os.WriteFile(logPath, []byte(logContent), 0o600); err != nil {
			t.Fatalf("write log fixture: %v", err)
		}
	}

	cfg := &config.Config{
		// A valid LLM block whose BaseURL points at the fake server, so the real
		// anthropic client returns the canned note without hitting the network.
		LLM: config.LLMConfig{
			Provider: "anthropic", Model: "test-model", APIKey: "test-key",
			MaxTokens: 4096, Temperature: 0.3, RequestTimeoutSec: 10,
			BaseURL: llmSrv.URL,
		},
		Paths: config.PathsConfig{LogFile: logPath, ObsidianVault: vaultDir},
		Agent: config.AgentConfig{
			ArxivCategory:         "cs.AI",
			ArxivBaseURL:          arxiv.URL,
			ArxivHTMLBaseURL:      htmlSrv.URL,
			MaxContentBytes:       52428800,
			FetchLimit:            20,
			DisplayLimit:          5,
			UserAgent:             "arxiv-explainer-agent/integration",
			RequestTimeoutSec:     5,
			MinRequestIntervalSec: 1,
			MaxRetries:            2,
		},
	}

	handler, err := server.Handler(cfg)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	app := httptest.NewServer(handler)
	t.Cleanup(app.Close)
	return testEnv{app: app, logPath: logPath, vaultDir: vaultDir}
}

// runDiscovery POSTs /discover then polls /status until terminal.
func runDiscovery(t *testing.T, app *httptest.Server) orchestrator.StatusResponse {
	t.Helper()

	postRes, err := http.Post(app.URL+"/discover", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /discover: %v", err)
	}
	defer postRes.Body.Close()
	if postRes.StatusCode != http.StatusOK {
		t.Fatalf("discover status: %d", postRes.StatusCode)
	}
	var trigger struct {
		SessionID string `json:"session_id"`
	}
	decode(t, postRes.Body, &trigger)
	if trigger.SessionID == "" {
		t.Fatal("empty session id")
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get(app.URL + "/status/" + trigger.SessionID)
		if err != nil {
			t.Fatalf("GET /status: %v", err)
		}
		var status orchestrator.StatusResponse
		decode(t, res.Body, &status)
		res.Body.Close()
		if status.Stage == models.StageSelection || status.Stage == models.StageFailed {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for terminal stage")
	return orchestrator.StatusResponse{}
}

func decode(t *testing.T, r io.Reader, v any) {
	t.Helper()
	if err := json.NewDecoder(r).Decode(v); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

// TestDiscoveryEndToEndFirstRun: no processed.json, 7 papers fetched, capped to 5.
func TestDiscoveryEndToEndFirstRun(t *testing.T) {
	env := setup(t, buildFeed(7), "")
	status := runDiscovery(t, env.app)

	if status.Stage != models.StageSelection {
		t.Fatalf("stage: want selection, got %s (error=%q)", status.Stage, status.Error)
	}
	if len(status.Candidates) != 5 {
		t.Fatalf("expected 5 candidates (capped), got %d", len(status.Candidates))
	}
	// newest-first ordering preserved from the feed
	if status.Candidates[0].ID != "2401.0001" {
		t.Errorf("first candidate ID = %q", status.Candidates[0].ID)
	}
	if status.Candidates[0].PDFURL == "" || status.Candidates[0].Title == "" {
		t.Errorf("candidate missing fields: %#v", status.Candidates[0])
	}
}

// TestDiscoveryDedup: a processed paper must never re-surface.
func TestDiscoveryDedup(t *testing.T) {
	// Mark 2401.0001 processed; feed has 3 papers -> expect 2 returned, 0001 gone.
	log := `{"processed":[{"paper_id":"2401.0001","title":"Paper 1"}]}`
	env := setup(t, buildFeed(3), log)
	status := runDiscovery(t, env.app)

	if len(status.Candidates) != 2 {
		t.Fatalf("expected 2 candidates after dedup, got %d", len(status.Candidates))
	}
	for _, c := range status.Candidates {
		if c.ID == "2401.0001" {
			t.Fatal("processed paper 2401.0001 was re-surfaced")
		}
	}
	// fewer than display_limit -> notice present
	if status.Notice == "" {
		t.Error("expected a fewer-than-limit notice")
	}
}

// discoverToSelection runs discovery and returns the session ID once it reaches
// the selection stage with candidates.
func discoverToSelection(t *testing.T, app *httptest.Server) (string, orchestrator.StatusResponse) {
	t.Helper()
	postRes, err := http.Post(app.URL+"/discover", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /discover: %v", err)
	}
	defer postRes.Body.Close()
	var trigger struct {
		SessionID string `json:"session_id"`
	}
	decode(t, postRes.Body, &trigger)

	var status orchestrator.StatusResponse
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get(app.URL + "/status/" + trigger.SessionID)
		if err != nil {
			t.Fatalf("GET /status: %v", err)
		}
		decode(t, res.Body, &status)
		res.Body.Close()
		if status.Stage == models.StageSelection {
			return trigger.SessionID, status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for selection")
	return "", status
}

// pollUntil polls /status until pred is satisfied or the deadline passes.
func pollUntil(t *testing.T, app *httptest.Server, sessionID string, pred func(orchestrator.StatusResponse) bool) orchestrator.StatusResponse {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var status orchestrator.StatusResponse
	for time.Now().Before(deadline) {
		res, err := http.Get(app.URL + "/status/" + sessionID)
		if err != nil {
			t.Fatalf("GET /status: %v", err)
		}
		decode(t, res.Body, &status)
		res.Body.Close()
		if pred(status) {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out; last stage=%s", status.Stage)
	return status
}

func selectPaper(t *testing.T, app *httptest.Server, sessionID, paperID string) {
	t.Helper()
	body := `{"session_id":"` + sessionID + `","paper_id":"` + paperID + `"}`
	res, err := http.Post(app.URL+"/process", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /process: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("process status: %d", res.StatusCode)
	}
}

// TestProcessEndToEndComplete: the full Phase 4 pipeline — select a paper, run
// extraction → generation (fake LLM) → atomic vault write → complete, then GET
// /result and assert the on-disk note has valid YAML frontmatter.
func TestProcessEndToEndComplete(t *testing.T) {
	env := setup(t, buildFeed(5), "")
	sessionID, status := discoverToSelection(t, env.app)
	paperID := status.Candidates[0].ID

	selectPaper(t, env.app, sessionID, paperID)
	final := pollUntil(t, env.app, sessionID, func(s orchestrator.StatusResponse) bool {
		return s.Stage == models.StageComplete || s.Stage == models.StageFailed
	})
	if final.Stage != models.StageComplete {
		t.Fatalf("stage: want complete, got %s (error=%q)", final.Stage, final.Error)
	}

	// GET /result returns content + vault path + tokens.
	res, err := http.Get(env.app.URL + "/result/" + sessionID)
	if err != nil {
		t.Fatalf("GET /result: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/result status: %d", res.StatusCode)
	}
	var result orchestrator.ResultResponse
	decode(t, res.Body, &result)
	if !strings.Contains(result.Content, "## Problem Statement") {
		t.Fatalf("result content missing sections: %.60q", result.Content)
	}
	if result.VaultFile == "" || result.TokensUsed != 2000 {
		t.Fatalf("unexpected result meta: file=%q tokens=%d", result.VaultFile, result.TokensUsed)
	}

	// The note exists on disk under {vault}/AI Papers with valid frontmatter.
	raw, err := os.ReadFile(result.VaultFile)
	if err != nil {
		t.Fatalf("read vault note: %v", err)
	}
	if !strings.HasPrefix(string(raw), "---\n") {
		t.Fatal("note missing YAML frontmatter")
	}
	if !strings.Contains(string(raw), "category: \"cs.AI\"") {
		t.Fatalf("frontmatter missing category from config: %.200q", string(raw))
	}
	if _, err := os.Stat(result.VaultFile + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("atomic write left a .tmp: %v", err)
	}

	// processed.json gained the entry only after the successful write.
	logRaw, err := os.ReadFile(env.logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(logRaw), paperID) {
		t.Fatalf("processed log missing paper %q: %s", paperID, string(logRaw))
	}
}

// TestProcessEndToEndVaultFailure: when the vault write fails (vault path is a
// regular file, so MkdirAll can't create "AI Papers"), the session fails, no
// note is written, and processed.json is NOT updated — so the paper re-surfaces.
func TestProcessEndToEndVaultFailure(t *testing.T) {
	env := setupWith(t, buildFeed(5), "", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(latexmlSample))
	})
	// Sabotage the vault: make the vault dir path a FILE, so MkdirAll("AI Papers")
	// under it fails with ENOTDIR.
	if err := os.WriteFile(env.vaultDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("sabotage vault: %v", err)
	}

	sessionID, status := discoverToSelection(t, env.app)
	paperID := status.Candidates[0].ID
	selectPaper(t, env.app, sessionID, paperID)

	final := pollUntil(t, env.app, sessionID, func(s orchestrator.StatusResponse) bool {
		return s.Stage == models.StageComplete || s.Stage == models.StageFailed
	})
	if final.Stage != models.StageFailed {
		t.Fatalf("stage: want failed on vault error, got %s", final.Stage)
	}
	// No processed.json (write never succeeded) → paper still unprocessed.
	if _, err := os.Stat(env.logPath); !os.IsNotExist(err) {
		t.Fatalf("processed log must not exist on vault failure: %v", err)
	}
	// /result stays 404 (never completed).
	res, err := http.Get(env.app.URL + "/result/" + sessionID)
	if err != nil {
		t.Fatalf("GET /result: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("/result should 404 on failure, got %d", res.StatusCode)
	}
}

// TestProcessEndToEnd404RePick: HTML server 404s → session returns to selection
// with a recoverable notice and candidates intact (re-pick without restart).
func TestProcessEndToEnd404RePick(t *testing.T) {
	env := setupWith(t, buildFeed(5), "", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	sessionID, status := discoverToSelection(t, env.app)
	nCandidates := len(status.Candidates)

	selectPaper(t, env.app, sessionID, status.Candidates[0].ID)
	got := pollUntil(t, env.app, sessionID, func(s orchestrator.StatusResponse) bool {
		return s.Stage == models.StageSelection && s.Notice != ""
	})
	if len(got.Candidates) != nCandidates {
		t.Fatalf("candidates must be preserved on re-pick: want %d, got %d", nCandidates, len(got.Candidates))
	}
	if !got.Recoverable {
		t.Fatal("re-pick must be recoverable")
	}
}

// TestHealthStillWorks: the Phase 1 endpoint survives the Phase 2 wiring.
func TestHealthStillWorks(t *testing.T) {
	env := setup(t, buildFeed(1), "")
	res, err := http.Get(env.app.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("health status: %d", res.StatusCode)
	}
}
