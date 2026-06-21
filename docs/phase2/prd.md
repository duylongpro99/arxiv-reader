# Phase 2 — arXiv Discovery & Duplicate Detection
## ArXiv AI Paper Explainer Agent

---

## Intent

The product's first promise to the user is: **"I will find what's new and worth your attention, without repeating what you've already seen."** Phase 2 delivers exactly that — and nothing more.

This phase is the entry point of the user-facing workflow. A practitioner triggers the agent, and within seconds sees a curated list of 5 unprocessed AI papers from arXiv. Every decision here serves two goals: reliable discovery and trustworthy deduplication. The user must be able to trust that the list is fresh, relevant, and never redundant.

No paper is read. No PDF is fetched. No LLM is called. This phase is deliberately narrow — it proves the discovery pipeline works before any expensive operations are introduced.

---

# Part 1 — Product Requirements

## 1. Problem Statement

A practitioner wants to know what new AI papers appeared on arXiv since their last session — without manually browsing arXiv, without seeing papers they've already processed, and without being overwhelmed by volume. They need a trusted, curated entry point that respects their time and attention.

Phase 2 solves the discovery and filtering problem. It answers: *"What should I look at today that I haven't already seen?"*

## 2. Target Users

**Primary:** Technical practitioners — ML engineers and developers who:
- Want to stay current with `cs.AI` research without daily manual arXiv browsing
- Have already processed some papers and don't want duplicates surfaced
- Expect the tool to do the filtering work, not them

## 3. User Stories

- As a practitioner, I want to click a single trigger button so that the agent fetches the latest AI papers without me visiting arXiv manually.
- As a practitioner, I want to see exactly 5 paper candidates so that my selection decision is focused, not overwhelming.
- As a practitioner, I want already-processed papers excluded automatically so that I never see the same paper twice.
- As a practitioner, I want each candidate to show title, authors, abstract snippet, and date so that I have enough context to make a selection decision.
- As a practitioner, I want a clear error message if discovery fails so that I know what went wrong and whether I can retry.

## 4. Functional Requirements

### F1 — Trigger Action
- A single "Find New Papers" button in the UI initiates the discovery pipeline
- The button is disabled and shows a loading state while discovery is running
- The pipeline is stateful — a session ID is created and tracked from this point forward

### F2 — arXiv Paper Fetch
- Agent queries arXiv API for the most recent papers in `cs.AI` category
- Fetches up to 20 papers per request (buffer to account for already-processed ones)
- Papers are ordered by submission date, most recent first
- Each paper record contains: arXiv ID, title, authors, abstract, PDF URL, published date

### F3 — Duplicate Detection
- Agent reads the local `processed.json` log file
- Cross-references fetched paper IDs against the log
- Returns only papers NOT present in the log
- Returns the top 5 unprocessed papers ordered by recency
- If fewer than 5 unprocessed papers exist, returns however many are available with a notice

### F4 — Candidate Display
- UI displays each candidate paper with:
  - Title (full)
  - Authors (comma-separated, all authors)
  - Abstract snippet (first 300 characters + ellipsis if truncated)
  - Published date (human-readable: "June 7, 2026")
  - arXiv ID badge (e.g. `2401.12345`)
- Each candidate has a "Select" button (inactive until discovery completes)
- UI transitions to selection state once candidates are displayed

### F5 — Pipeline Status
- `GET /status/:sessionId` returns current pipeline stage and any error
- Frontend polls this endpoint during discovery to show live progress
- Stage labels shown to user:
  - `"Connecting to arXiv..."`
  - `"Filtering new papers..."`
  - `"Ready — select a paper"`

### F6 — Error Handling
- If arXiv API returns an error after all retries: surface clear message with retry option
- If fewer than 5 unprocessed papers found: surface available candidates with a count notice
- If `processed.json` does not exist yet: treat as empty log (first run), create the file

## 5. Non-Functional Requirements

- **Responsiveness** — discovery completes in under 10 seconds under normal network conditions
- **Reliability** — transient arXiv 429 errors are retried automatically without user intervention
- **Transparency** — the user always knows what stage the pipeline is in
- **Trust** — duplicate detection must be 100% reliable; a previously processed paper must never be re-surfaced

## 6. Success Metrics

- Trigger → candidate list displayed in under 10 seconds on a normal connection
- Zero previously processed papers appear in the candidate list across multiple runs
- A first run with no `processed.json` works without error
- arXiv 429 errors retry automatically and succeed without user action in the majority of cases

## 7. Scope & Non-Goals

**In scope:**
- arXiv `cs.AI` category fetch (top 20, filter to 5 unprocessed)
- `processed.json` read and filter
- Candidate list UI with paper metadata
- Pipeline session creation and status polling
- Error surfaces for discovery failures

**Out of scope:**
- Paper selection handling (Phase 3)
- PDF fetching (Phase 3)
- Any LLM calls (Phase 3+)
- Writing to `processed.json` (Phase 4 — only after successful vault write)
- Any content generation (Phase 4+)
- Relevance ranking or keyword filtering (explicitly deferred — non-goal)

## 8. Open Questions

None for this phase. All requirements are fully defined.

---

# Part 2 — Architecture

## Intent

The architecture in this phase must be simple enough to be completely reliable, yet structured cleanly enough that Phase 3 can slot in PDF fetching and LLM calls without restructuring anything. The two components introduced here — `DiscoveryTool` and `LogCheckTool` — each do exactly one thing. The Orchestrator introduced here is deliberately thin: it coordinates, it does not process.

---

## 1. System Overview

```
User clicks "Find New Papers"
    │
    ▼
Next.js UI → POST /api/trigger → Next.js API Route
    │
    ▼
Next.js API Route → POST localhost:8080/discover → Go Orchestrator
    │
    ▼
Go Orchestrator
    ├── Creates PipelineSession { stage: "discovery" }
    ├── Runs DiscoveryTool → fetches top 20 cs.AI papers from arXiv
    ├── Runs LogCheckTool  → filters already-processed paper IDs
    └── Updates session { stage: "selection", candidates: top 5 }
    │
    ▼
Response → Next.js → Renders candidate list UI
    │
    ▼
[Phase 3 picks up here: user selects a paper]


Progress polling (parallel):
Next.js polls GET /api/status → GET /status/:sessionId → { stage, error }
```

---

## 2. Component Breakdown

### 2.1 Next.js — Trigger UI

**Intent:** Give the user a single, unambiguous action that starts the workflow. The UI must communicate clearly that something is happening and what to expect next.

**Why a single button:** Multiple entry points or options at this stage introduce cognitive load before the user has seen anything useful. One button, one action, clear result.

**Responsibilities:**
- Render "Find New Papers" trigger button
- On click: call `POST /api/trigger`, store returned `session_id`
- Begin polling `GET /api/status/:sessionId` every 2 seconds via TanStack Query
- Show loading state with live stage label during discovery
- Render candidate list once `stage === "selection"`
- Show error state with retry button if `stage === "failed"`

**Component structure:**
```
/app/page.tsx
  └── <DiscoveryPanel>
        ├── <TriggerButton />          # "Find New Papers" button
        ├── <ProgressIndicator />      # stage label during discovery
        ├── <CandidateList />          # rendered once candidates arrive
        │     └── <PaperCard />        # one per candidate paper
        └── <ErrorBanner />            # shown on failure
```

**API calls (Next.js → Go backend):**
```typescript
// POST /api/trigger
// → Go POST /discover
// ← { session_id: string, candidates: Paper[] }

// GET /api/status?sessionId=xxx
// → Go GET /status/:sessionId
// ← { stage: string, error: string | null }
```

**TanStack Query polling:**
```typescript
// Polls every 2 seconds, stops when stage is "selection" or "failed"
const { data: status } = useQuery({
  queryKey: ['status', sessionId],
  queryFn: () => fetchStatus(sessionId),
  refetchInterval: (data) =>
    data?.stage === 'selection' || data?.stage === 'failed' ? false : 2000,
  enabled: !!sessionId,
})
```

**Dependencies:** TanStack Query 5.101.0, Next.js API routes

---

### 2.2 Next.js API Routes (Proxy Layer)

**Intent:** Next.js API routes act as a thin, typed proxy between the frontend and Go backend. They exist to keep the Go backend URL out of the browser, enforce a consistent frontend-facing API shape, and provide a single place to add request validation if needed later.

**Why a proxy layer:** The Go backend is a local service — its address should not be hardcoded in client-side JavaScript. The proxy layer abstracts the backend address behind a stable frontend API.

**Routes (Phase 2):**
```typescript
// /app/api/trigger/route.ts
POST /api/trigger
  → POST http://localhost:8080/discover
  ← { session_id, candidates: Paper[] }

// /app/api/status/route.ts
GET /api/status?sessionId=xxx
  → GET http://localhost:8080/status/{sessionId}
  ← { stage, error }
```

**Shared TypeScript types:**
```typescript
// /lib/types.ts
interface Paper {
  id: string
  title: string
  authors: string[]
  abstract: string
  pdfUrl: string
  published: string   // ISO date string
}

interface PipelineStatus {
  stage: PipelineStage
  candidates?: Paper[]
  iteration?: number
  error?: string
  recoverable?: boolean
}

type PipelineStage =
  | 'discovery'
  | 'selection'
  | 'fetching_pdf'
  | 'generating'
  | 'reviewing'
  | 'revising'
  | 'writing'
  | 'complete'
  | 'failed'
```

**Dependencies:** Next.js API routes, `/lib/types.ts`

---

### 2.3 Go Orchestrator — Discovery Endpoint

**Intent:** The Orchestrator is the conductor, not a performer. It sequences tools, maintains session state, and surfaces results. It contains no business logic of its own — that belongs in the tools.

**Why in-memory session store:** Sessions are short-lived (minutes). A database would add infrastructure complexity with zero benefit for a local, single-user tool. A simple `sync.Map` is thread-safe and sufficient.

**Responsibilities (Phase 2):**
- Expose `POST /discover` and `GET /status/:sessionId`
- Create and store `PipelineSession` in memory
- Sequence `DiscoveryTool` → `LogCheckTool`
- Update session stage at each step
- Return candidates to frontend

**Session store:**
```go
// /internal/orchestrator/orchestrator.go
type Orchestrator struct {
    sessions sync.Map   // map[string]*models.PipelineSession
    config   *config.Config
}

func (o *Orchestrator) getSession(id string) (*models.PipelineSession, bool)
func (o *Orchestrator) setSession(session *models.PipelineSession)
func (o *Orchestrator) newSession() *models.PipelineSession  // generates UUID session ID
```

**`POST /discover` handler:**
```go
func (o *Orchestrator) HandleDiscover(w http.ResponseWriter, r *http.Request) {
    session := o.newSession()
    session.Stage = models.StageDiscovery
    o.setSession(session)

    // Run discovery pipeline
    papers, err := o.discoveryTool.FetchPapers(r.Context(), 20)
    // handle error → session.Stage = StageFailed

    session.Stage = models.StageDiscovery  // update for status polling
    filtered, err := o.logCheckTool.FilterUnprocessed(papers)
    // handle error → session.Stage = StageFailed

    // Cap to top 5
    candidates := filtered
    if len(candidates) > o.config.Agent.PaperFetchLimit {
        candidates = candidates[:o.config.Agent.PaperFetchLimit]
    }

    session.Candidates = candidates
    session.Stage = models.StageSelection
    o.setSession(session)

    json.NewEncoder(w).Encode(DiscoverResponse{
        SessionID:  session.SessionID,
        Candidates: candidates,
    })
}
```

**`GET /status/:sessionId` handler:**
```go
func (o *Orchestrator) HandleStatus(w http.ResponseWriter, r *http.Request) {
    sessionID := r.PathValue("sessionId")
    session, ok := o.getSession(sessionID)
    if !ok {
        http.Error(w, "session not found", http.StatusNotFound)
        return
    }
    json.NewEncoder(w).Encode(StatusResponse{
        Stage:       session.Stage,
        Iteration:   session.Iterations,
        Error:       session.Error,
        Recoverable: session.Recoverable,
    })
}
```

**Dependencies:** DiscoveryTool, LogCheckTool, `models`, `config`

---

### 2.4 DiscoveryTool

**Intent:** This tool owns the relationship with arXiv. It knows how to talk to the API, how to parse the response, and how to behave politely (rate limiting, retries). Nothing outside this tool needs to know how arXiv works.

**Why fetch 20, return all:** Fetching more than 5 gives LogCheckTool enough candidates to filter from, ensuring we can surface 5 unprocessed papers even after many runs. 20 is a practical buffer — large enough to always have options, small enough to stay within arXiv's rate limits in a single request.

**Interface:**
```go
// /internal/tools/discovery.go

type DiscoveryTool struct {
    config     *config.Config
    httpClient *http.Client
}

func (t *DiscoveryTool) FetchPapers(ctx context.Context, limit int) ([]models.Paper, error)
```

**arXiv API query:**
```
GET https://export.arxiv.org/api/query
  ?search_query=cat:cs.AI
  &sortBy=submittedDate
  &sortOrder=descending
  &max_results=20
  &start=0
Headers:
  User-Agent: arxiv-explainer-agent/1.0 (contact: your@email.com)
```

**Atom/XML parsing:**
```go
type arxivFeed struct {
    Entries []arxivEntry `xml:"entry"`
}

type arxivEntry struct {
    ID        string       `xml:"id"`
    Title     string       `xml:"title"`
    Authors   []arxivAuthor `xml:"author"`
    Summary   string       `xml:"summary"`
    Published string       `xml:"published"`
    Links     []arxivLink  `xml:"link"`
}

// Extract arXiv ID from entry ID URL:
// "http://arxiv.org/abs/2401.12345v1" → "2401.12345"
func extractArxivID(rawID string) string
```

**Rate limiting and retry:**
```go
// Minimum 3-second delay between requests (arXiv requirement)
// Retry on 429 with exponential backoff: 3s → 6s → 12s (max 3 retries)
// Log each retry:
// {"level":"WARN","component":"discovery","attempt":2,"backoff_ms":6000,"error":"429"}
```

**Error types:**
```go
var (
    ErrArxivRateLimit   = errors.New("arXiv rate limit exceeded after retries")
    ErrArxivUnavailable = errors.New("arXiv API unavailable")
    ErrArxivParse       = errors.New("failed to parse arXiv response")
)
```

**Dependencies:** `net/http`, `encoding/xml`, `config`

---

### 2.5 LogCheckTool

**Intent:** This tool is the system's memory. It knows which papers have already been explained, and it protects the user from ever seeing the same paper twice. Its read path is used here in Phase 2; its write path (`MarkAsProcessed`) is used in Phase 4.

**Why a flat JSON file over a database:** The processed log is a simple append-only list of paper IDs. A database would add tooling overhead (migrations, drivers, startup) for a data structure that is effectively a set. The JSON file is human-readable, manually editable if needed, and trivially portable.

**Interface:**
```go
// /internal/tools/logcheck.go

type LogCheckTool struct {
    config *config.Config
}

func (t *LogCheckTool) FilterUnprocessed(papers []models.Paper) ([]models.Paper, error)
func (t *LogCheckTool) MarkAsProcessed(paper models.Paper, vaultFile string) error
```

**`processed.json` structure:**
```json
{
  "processed": [
    {
      "paper_id": "2401.12345",
      "title": "Paper Title",
      "processed_at": "2026-06-07T10:30:00Z",
      "vault_file": "2026-06-07_2401.12345_paper-title-slug.md"
    }
  ]
}
```

**FilterUnprocessed logic:**
```go
func (t *LogCheckTool) FilterUnprocessed(papers []models.Paper) ([]models.Paper, error) {
    log, err := t.readLog()
    if err != nil && os.IsNotExist(err) {
        // First run — no log file yet, all papers are unprocessed
        return papers, nil
    }

    processedIDs := make(map[string]bool)
    for _, entry := range log.Processed {
        processedIDs[entry.PaperID] = true
    }

    var unprocessed []models.Paper
    for _, p := range papers {
        if !processedIDs[p.ID] {
            unprocessed = append(unprocessed, p)
        }
    }
    return unprocessed, nil
}
```

**File handling:**
- Read: `os.ReadFile` → `json.Unmarshal`
- Write (Phase 4): `json.Marshal` → atomic write (temp file → rename)
- Parent directory created automatically on first write if it doesn't exist

**Dependencies:** `os`, `encoding/json`, `config`

---

## 3. Data Model

Phase 2 uses models defined in Phase 1. No new persistent entities are introduced.

**In-memory only (Phase 2):**
```go
// PipelineSession — updated at each stage
PipelineSession {
    SessionID:  "uuid-v4",
    Stage:      "selection",
    Candidates: []Paper{...},  // 5 unprocessed papers
    StartedAt:  time.Now(),
}
```

**Read from disk (Phase 2):**
```json
// processed.json — read-only in this phase
{
  "processed": [
    { "paper_id": "2401.12345", ... }
  ]
}
```

**Written to disk (Phase 2):**
- Nothing. `processed.json` is only written in Phase 4 after successful vault write.

---

## 4. Data Flow

### Discovery Flow

```
1. User clicks "Find New Papers"
      │
      ▼
2. Next.js → POST /api/trigger
      │
      ▼
3. Next.js API route → POST localhost:8080/discover
      │
      ▼
4. Orchestrator.HandleDiscover()
      │
      ├── Creates PipelineSession { stage: "discovery" }
      │
      ├── DiscoveryTool.FetchPapers(ctx, 20)
      │     ├── GET https://export.arxiv.org/api/query?cat=cs.AI&max=20...
      │     ├── Parse Atom/XML response
      │     └── Return []Paper (up to 20)
      │
      ├── LogCheckTool.FilterUnprocessed(papers)
      │     ├── Read processed.json (or treat as empty if not found)
      │     ├── Build processedIDs set
      │     └── Return []Paper (unprocessed only)
      │
      ├── Cap to top 5 by recency
      │
      └── Update session { stage: "selection", candidates: []Paper }
      │
      ▼
5. Response: { session_id, candidates: []Paper }
      │
      ▼
6. Next.js stores session_id, renders <CandidateList />
```

### Status Polling Flow (parallel to above)

```
Next.js polls GET /api/status?sessionId=xxx every 2 seconds
      │
      ▼
Go: GET /status/:sessionId
      │
      ▼
Orchestrator reads session from sync.Map
      │
      ▼
Returns { stage: "discovery" | "selection" | "failed", error }
      │
      ▼
Next.js updates stage label:
  "discovery" → "Connecting to arXiv..."
  "selection" → stops polling, renders candidates
  "failed"    → stops polling, shows error + retry button
```

### Error Flow

```
DiscoveryTool.FetchPapers() returns ErrArxivRateLimit after 3 retries
      │
      ▼
Orchestrator sets session {
    stage: "failed",
    error: "arXiv is rate limiting requests. Please try again in a minute.",
    recoverable: true
}
      │
      ▼
Next.js status poll receives { stage: "failed", recoverable: true }
      │
      ▼
UI shows error banner with retry button
```

---

## 5. Tech Stack

No new technologies introduced in Phase 2. All stack choices were established in Phase 1.

**Key stdlib packages used (Go):**
- `encoding/xml` — Atom/XML parsing for arXiv API response
- `encoding/json` — `processed.json` read/write
- `net/http` — arXiv API calls
- `sync` — thread-safe in-memory session store (`sync.Map`)
- `os` — file system operations for log file

---

## 6. Integration Points

### arXiv API

**Endpoint:** `https://export.arxiv.org/api/query`
**Auth:** None
**Format:** Atom/XML

**Request:**
```
GET https://export.arxiv.org/api/query
  ?search_query=cat:cs.AI
  &sortBy=submittedDate
  &sortOrder=descending
  &max_results=20
Headers:
  User-Agent: arxiv-explainer-agent/1.0
```

**Constraints enforced:**
- 3-second minimum delay between requests
- Exponential backoff on 429: 3s → 6s → 12s, max 3 retries
- 10-second request timeout
- Single connection (no concurrent arXiv requests)

**Key parsing note:** arXiv entry IDs are URLs (`http://arxiv.org/abs/2401.12345v1`). Extract the numeric ID by splitting on `/abs/` and stripping the version suffix (`v1`, `v2`, etc.).

---

## 7. Cross-Cutting Concerns

### Error Handling

| Failure | Behaviour | Recoverable |
|---|---|---|
| arXiv API 429 after 3 retries | Surface "rate limited" error, session → failed | Yes |
| arXiv API 5xx | Surface "unavailable" error, session → failed | Yes |
| arXiv XML parse failure | Surface "unexpected response" error, session → failed | Yes |
| `processed.json` not found | Treat as empty — first run scenario | N/A |
| `processed.json` malformed | Surface "log file corrupted" error | No — manual fix required |
| Fewer than 5 unprocessed papers | Return available count with UI notice | N/A |

### Observability

```json
{"level":"INFO","msg":"discovery started","session_id":"abc123"}
{"level":"INFO","msg":"arxiv fetch complete","count":20,"duration_ms":1240}
{"level":"INFO","msg":"log check complete","unprocessed":12,"returning":5}
{"level":"INFO","msg":"discovery complete","session_id":"abc123","stage":"selection"}

{"level":"WARN","msg":"arxiv rate limited","attempt":1,"backoff_ms":3000}
{"level":"ERROR","msg":"discovery failed","session_id":"abc123","error":"rate limit exceeded after 3 retries"}
```

### Security
- No user input is included in arXiv API queries (category is from config, not user-supplied)
- `processed.json` path is validated against configured base path before read/write
- No paper data is persisted in this phase

---

## 8. Risks & Tradeoffs

| ID | Risk/Tradeoff | Severity | Mitigation |
|---|---|---|---|
| R1 | arXiv API structure changes (XML schema update) | Low | `encoding/xml` is flexible; minor schema changes are unlikely to break parsing. Monitor arXiv API announcements. |
| R2 | 20-paper buffer insufficient after many runs | Low | If all 20 fetched papers are processed, surface a "no new papers" message. User can retry later when new papers are submitted. |
| R3 | `processed.json` corruption | Low | File is append-only JSON. Corruption is unlikely but possible (e.g. power loss during write — mitigated in Phase 4 with atomic writes). For reads, surface a named error with instructions to inspect the file. |
| T1 | Synchronous discovery (blocks HTTP response) | Accepted | Discovery takes 1–2 seconds — well within acceptable response time. Async complexity not justified. Status polling is still implemented for consistency with later phases. |
| T2 | No relevance ranking — recency only | Accepted | Explicitly deferred per PRD. Recency is a reliable, predictable signal. Ranking adds complexity and subjectivity without clear benefit at this stage. |

---

## Exit Criteria

All of the following must be true before Phase 3 begins:

- [ ] Clicking "Find New Papers" returns exactly 5 unprocessed `cs.AI` papers from arXiv
- [ ] Papers are ordered by submission date, most recent first
- [ ] Each paper card displays: title, authors, abstract snippet (300 chars), published date, arXiv ID
- [ ] Running discovery a second time after Phase 4 processes a paper does not re-surface that paper
- [ ] First run with no `processed.json` works without error — file absence treated as empty log
- [ ] arXiv 429 retries automatically up to 3 times before surfacing an error
- [ ] `stage === "failed"` surfaces a clear, human-readable error message in the UI
- [ ] `stage === "failed"` with `recoverable: true` shows a retry button
- [ ] Status polling stops correctly when stage reaches `"selection"` or `"failed"`
- [ ] All discovery events logged with `session_id`, `stage`, and `duration_ms`
