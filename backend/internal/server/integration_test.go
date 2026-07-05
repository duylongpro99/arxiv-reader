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

// setup wires a real server.Handler against a fake arXiv (Atom API) and a temp
// log file. The paper-HTML server always serves 200 sample HTML.
func setup(t *testing.T, feed string, logContent string) (*httptest.Server, string) {
	return setupWith(t, feed, logContent, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(latexmlSample))
	})
}

// setupWith is setup with a caller-controlled paper-HTML handler (e.g. to force
// a 404 for the re-pick path).
func setupWith(t *testing.T, feed, logContent string, htmlHandler http.HandlerFunc) (*httptest.Server, string) {
	t.Helper()

	arxiv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
	t.Cleanup(arxiv.Close)

	htmlSrv := httptest.NewServer(htmlHandler)
	t.Cleanup(htmlSrv.Close)

	logPath := filepath.Join(t.TempDir(), "processed.json")
	if logContent != "" {
		if err := os.WriteFile(logPath, []byte(logContent), 0o600); err != nil {
			t.Fatalf("write log fixture: %v", err)
		}
	}

	cfg := &config.Config{
		// A valid LLM block is required so orchestrator.New (which constructs the
		// client) succeeds; the client is never invoked in these Phase 3 tests.
		LLM: config.LLMConfig{
			Provider: "anthropic", Model: "test-model", APIKey: "test-key",
			MaxTokens: 100, Temperature: 0.3, RequestTimeoutSec: 10,
		},
		Paths: config.PathsConfig{LogFile: logPath},
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
	return app, logPath
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
	app, _ := setup(t, buildFeed(7), "")
	status := runDiscovery(t, app)

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
	app, _ := setup(t, buildFeed(3), log)
	status := runDiscovery(t, app)

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

// TestProcessEndToEndExtracting: select a paper whose HTML is served 200 → the
// session transitions to (and stays at) extracting after the Markdown is stored.
func TestProcessEndToEndExtracting(t *testing.T) {
	app, _ := setup(t, buildFeed(5), "")
	sessionID, status := discoverToSelection(t, app)

	selectPaper(t, app, sessionID, status.Candidates[0].ID)
	got := pollUntil(t, app, sessionID, func(s orchestrator.StatusResponse) bool {
		return s.Stage == models.StageExtracting
	})
	if got.Stage != models.StageExtracting {
		t.Fatalf("stage: want extracting, got %s (error=%q)", got.Stage, got.Error)
	}
	// Give the pipeline a beat; a stored-Markdown success keeps it at extracting
	// (no failure). It must NOT flip to failed.
	time.Sleep(50 * time.Millisecond)
	final := pollUntil(t, app, sessionID, func(s orchestrator.StatusResponse) bool { return true })
	if final.Stage == models.StageFailed {
		t.Fatalf("extraction failed unexpectedly: %q", final.Error)
	}
}

// TestProcessEndToEnd404RePick: HTML server 404s → session returns to selection
// with a recoverable notice and candidates intact (re-pick without restart).
func TestProcessEndToEnd404RePick(t *testing.T) {
	app, _ := setupWith(t, buildFeed(5), "", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	sessionID, status := discoverToSelection(t, app)
	nCandidates := len(status.Candidates)

	selectPaper(t, app, sessionID, status.Candidates[0].ID)
	got := pollUntil(t, app, sessionID, func(s orchestrator.StatusResponse) bool {
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
	app, _ := setup(t, buildFeed(1), "")
	res, err := http.Get(app.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("health status: %d", res.StatusCode)
	}
}
