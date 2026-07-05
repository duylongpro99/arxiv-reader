# Phase 3 — Brainstorm Summary (REVISED)
## PDF → Markdown Text via arXiv HTML

> **Supersedes the page-as-image design in `docs/phase3/prd.md`.** This session pivoted
> the core mechanism away from PDF rasterization + vision LLM to arXiv-HTML → Markdown +
> text LLM. The PRD must be updated to match before/with implementation.

---

## 1. Problem Statement

A user has selected a paper. The system must acquire its full content and route it to the
configured LLM. Original PRD chose **page-as-image** (poppler `pdftoppm` → PNG per page →
vision LLM) for full visual fidelity. Decision reversed: the vision path is expensive, forces
a vision-capable model, drags in a system dependency (poppler), and adds a fragile disk
round-trip. We instead extract **clean Markdown text** and use any text LLM.

Explicitly accepted cost: diagrams, figures, and rendered equations are dropped for now.

## 2. Key Insight That Drove the Design

We are not converting arbitrary PDFs — we are converting **arXiv papers**, which are
two-column LaTeX. Naive PDF text extraction interleaves the two columns into unreadable
order (the #1 failure mode on this corpus). But arXiv publishes a **LaTeXML-rendered HTML
version** at `arxiv.org/html/{id}` — linear reading order, real section headings, tables,
captions, and math as MathML. A plain HTTP GET + pure-Go HTML→Markdown transform beats every
PDF-extraction path on quality **and** eliminates poppler/CGO.

**Verified during this session:**
- `GET https://arxiv.org/html/2312.00752` (bare id, no version) → 200, redirects to
  `/html/2312.00752v2`, returns full structured HTML. Go's `http.Client` follows the
  same-host redirect automatically → **`Paper.ID` as stored (version stripped) works as-is.**
- HTML is LaTeXML-generated: TOC, sections, equations, figures, tables, citations.

## 3. Evaluated Approaches

| Approach | Fidelity on arXiv | Deps | Verdict |
|---|---|---|---|
| **arXiv HTML → Markdown** ✅ | High — linear, structured, MathML | pure-Go HTML→MD lib | **CHOSEN** |
| pdftotext (poppler) → text | Low — 2-col order shaky, flat | poppler system dep | rejected |
| Pure-Go PDF lib (ledongthuc/pdf) | Lowest — 2-col interleave, no structure | none | rejected |
| Page-as-image + vision (old PRD) | Highest visual | poppler + vision model | reversed |
| marker / Docling (Python ML) | Highest text | heavy Python+models | rejected (KISS) |

**Decisions locked this session:**
- Extraction path: **arXiv HTML → Markdown (pure Go)**
- Cleanup level: **raw / minimal** — strip nav/boilerplate + references/appendix, let the LLM tolerate imperfections
- Diagrams/formulas: **leave a seam** — keep figure/table captions; defer images + rendered math

## 4. Recommended Solution

### 4.1 New tool — `PaperContentTool` (`/internal/tools/papercontent.go`)
Replaces both `PDFFetchTool` and `PDFRendererTool` from the old PRD.

```go
func (t *PaperContentTool) FetchMarkdown(ctx context.Context, arxivID string) (string, error)
```
- `GET {arxiv_html_base_url}/{arxivID}` (default base `https://arxiv.org/html`); client follows
  redirect to versioned URL.
- Wrap body read in `io.LimitReader` (size cap → clean error, no OOM).
- Convert HTML→Markdown with **`github.com/JohannesKaufmann/html-to-markdown/v2`** (pure Go).
- Minimal cleanup: strip header/footer/nav + LaTeXML chrome; trim bibliography + appendix;
  **keep figure/table captions**; collapse whitespace; strip `<math>` nodes to avoid MathML
  noise (see seam below).
- Reuse DiscoveryTool's retry/backoff + `User-Agent` politeness pattern (same domain).
- Errors: `ErrPaperHTMLNotFound` (404 → recoverable), `ErrPaperHTMLFailed`, timeout.

### 4.2 LLMClient interface — simplified (text-only)
```go
type CompletionRequest struct {
    SystemPrompt string
    UserPrompt   string
    DocumentText string   // was: PageImages [][]byte
    MaxTokens    int
    Temperature  float32
}
```
All three provider clients (Anthropic/OpenAI/Gemini) send plain text — no base64, no image
blocks (~half the code of the old PRD). **F4 vision requirement, `vision.go`,
`KnownVisionModels`, and `ValidateVisionSupport` are deleted.** Any text model is valid.

### 4.3 Data model + stages
- `PipelineSession`: drop `PDF []byte` / `PageImages [][]byte`; add mutex-guarded
  `markdownText string`. **Excluded from `Snapshot()`** (large; never shipped to frontend).
- New stage: `StageExtracting = "extracting"` — collapses old `fetching_pdf` + `rendering_pdf`
  into one. Frontend label: `"Extracting paper text..."`.
- ⚠️ Old PRD's `runPipeline` mutates `session.PDF`/`session.Stage` directly — **won't compile**
  against the real mutex-guarded model (`session.go:26`). Add accessors (`SetStage`,
  `SetMarkdown`) following the existing locking pattern.

### 4.4 Config
- Remove `pdf.dpi`.
- Add `agent.arxiv_html_base_url: https://arxiv.org/html` (testability — mirrors
  `arxiv_base_url`, lets tests point at an httptest server) + a content size cap. Reuse
  existing timeout/retry/user_agent knobs.

### 4.5 Dependencies
- **ADD:** `github.com/JohannesKaufmann/html-to-markdown/v2` (pure Go).
- **REMOVE (vs old PRD plan):** `poppler-utils` system dep, `ValidatePoppler`, `pdf.dpi`.
- Keep the three LLM SDKs (text usage).

## 5. The Seam (future re-inclusion, near-free)
LaTeXML emits `alttext="<original LaTeX>"` on every `<math>` node. So bringing equations back
later = read `alttext` instead of stripping — **LaTeX recovery is basically free from the HTML
path.** Captions retained now give figure/table context without images. This is why HTML beats
page-image even for the deferred features.

## 6. Risks & Tradeoffs

| ID | Risk / Tradeoff | Severity | Mitigation |
|---|---|---|---|
| R1 | Paper has no HTML rendering | Low | Near-100% coverage for *recent* cs.AI (discovery sorts newest-first). 404 → recoverable fail. |
| R2 | A rare no-HTML paper interrupts the run | Low–Med | **Resolved (§7): relax F1.** On 404, return to candidate list (same session) so user re-picks — no restart. |
| R3 | LaTeXML HTML rendering artifacts | Low | Minimal cleanup + LLM tolerance; strip MathML noise. |
| T1 | Diagrams/figures/rendered math dropped | Accepted | Captions kept; alttext seam for cheap math later. Lower ceiling on figure-heavy papers. |
| T2 | Depends on arXiv HTML endpoint availability | Low | Same domain already trusted for discovery; retry/backoff reused. |

## 7. No-HTML Fallback — RESOLVED
**Relax old PRD F1 (irreversible selection).** On HTML 404/empty, the pipeline returns the
session to the **`selection` stage** (candidates are already held in session) with a recoverable
notice, and the frontend re-enables the candidate list so the user picks another paper **without
restarting the session**. Given newest-first discovery, this rarely fires, but the re-pick path
removes the dead-end entirely.

State impact: `extracting` failure transitions back to `selection` (not `failed`), preserving
`candidates`; frontend clears `selectedId` so `<PaperCard>` buttons re-enable, and shows the
recoverable notice.

## 8. Success Metrics
- `arxiv.org/html/{id}` fetched + converted to Markdown for any recent cs.AI paper.
- Markdown has correct reading order (no 2-column interleave), real headings, captions kept.
- `LLMClient.Complete()` returns valid text response for all three providers with `DocumentText`.
- Switching `llm.provider` routes correctly — **no vision constraint anywhere**.
- No poppler, no CGO, no Python in the build.

## 9. Next Steps
1. Resolve §7 open decision.
2. Update `docs/phase3/prd.md` to this mechanism (docs-manager).
3. `/ck:plan` from this summary → phased implementation.
