package server_test

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/models"
	"github.com/maritime-ds/arxiv-reader/internal/orchestrator"
)

// enableTracing turns on the Recorder for an E2E run. DATABASE_URL (if set in the
// environment) wires the durable store; empty → in-memory-only tracing (the
// always-run CI path). This is a config tweak passed to setupWith.
func enableTracing(c *config.Config) {
	c.Tracing = config.TracingConfig{Enabled: true, BufferSize: 256}
	c.DatabaseURL = os.Getenv("DATABASE_URL")
}

// getSSEBody GETs the run's timeline stream and returns the full body. The run is
// terminal by the time we call, so the handler replays and closes (finite body).
// A client timeout guards against a hang if that ever regresses.
func getSSEBody(t *testing.T, appURL, sessionID string) string {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Get(appURL + "/runs/" + sessionID + "/events")
	if err != nil {
		t.Fatalf("GET /runs/:id/events: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("SSE status: %d", res.StatusCode)
	}
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read SSE body: %v", err)
	}
	return string(b)
}

// TestTimelineEndToEnd drives the full pipeline with tracing ON, then asserts the
// live SSE timeline tells the ordered story. It runs in BOTH modes: in-memory
// only (no DATABASE_URL — the CI default, exercising the degrade path) and, when
// DATABASE_URL is set, DB-backed with a persisted-read assertion. Either way the
// pipeline must complete and the timeline must stream — tracing never breaks the
// pipeline (a core success criterion).
func TestTimelineEndToEnd(t *testing.T) {
	env := setupWith(t, buildFeed(5), "", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(latexmlSample))
	}, enableTracing)

	sessionID, status := discoverToSelection(t, env.app)
	paperID := status.Candidates[0].ID
	selectPaper(t, env.app, sessionID, paperID)
	final := pollUntil(t, env.app, sessionID, func(s orchestrator.StatusResponse) bool {
		return s.Stage == models.StageComplete || s.Stage == models.StageFailed
	})
	if final.Stage != models.StageComplete {
		t.Fatalf("pipeline must complete with tracing on, got %s (err=%q)", final.Stage, final.Error)
	}

	// The SSE timeline must contain the ordered story beats. We assert presence
	// (not exact ordering) since the stream mixes replay + live tail; the unit
	// tests already pin the exact sequence.
	body := getSSEBody(t, env.app.URL, sessionID)
	for _, kind := range []string{
		"discovery.started", "tool.discovery.completed", "selection.chosen",
		"tool.papercontent.completed", "llm.explainer.completed",
		"tool.vaultwriter.completed", "run.completed",
	} {
		if !strings.Contains(body, `"type":"`+kind+`"`) {
			t.Fatalf("timeline missing %q event:\n%s", kind, body)
		}
	}
	// No secret/raw-HTML leakage: the fake API key must never appear, and the
	// extracted HTML body text is summarized (size + preview) not shipped whole.
	if strings.Contains(body, "test-key") {
		t.Fatal("API key leaked into the timeline stream")
	}

	if os.Getenv("DATABASE_URL") == "" {
		assertHistoryDegraded(t, env.app.URL)
	} else {
		assertHistoryPersisted(t, env.app.URL, sessionID, paperID)
	}
}

// assertHistoryDegraded verifies that with no DB the history REST endpoints
// report unavailable (503) cleanly — while the live timeline above still worked.
func assertHistoryDegraded(t *testing.T, appURL string) {
	t.Helper()
	res, err := http.Get(appURL + "/runs")
	if err != nil {
		t.Fatalf("GET /runs: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("no-DB /runs should be 503, got %d", res.StatusCode)
	}
}

// assertHistoryPersisted (DB mode) verifies the run + timeline are readable from
// Postgres after completion: /runs lists it and /runs/:id returns its events.
func assertHistoryPersisted(t *testing.T, appURL, sessionID, paperID string) {
	t.Helper()
	// Persistence is async; poll briefly for the run to appear in the list.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get(appURL + "/runs?limit=100")
		if err != nil {
			t.Fatalf("GET /runs: %v", err)
		}
		var list orchestrator.RunsListResponse
		decode(t, res.Body, &list)
		res.Body.Close()
		found := false
		for _, r := range list.Runs {
			if r.ID == sessionID {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline.Add(-50 * time.Millisecond)) {
			t.Fatalf("run %s never appeared in /runs", sessionID)
		}
		time.Sleep(50 * time.Millisecond)
	}

	res, err := http.Get(appURL + "/runs/" + sessionID)
	if err != nil {
		t.Fatalf("GET /runs/:id: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/runs/:id status: %d", res.StatusCode)
	}
	var detail orchestrator.RunDetailResponse
	decode(t, res.Body, &detail)
	if detail.Run.PaperID != paperID {
		t.Fatalf("persisted run paper = %q, want %q", detail.Run.PaperID, paperID)
	}
	if len(detail.Events) == 0 {
		t.Fatal("persisted run has no events")
	}
}
