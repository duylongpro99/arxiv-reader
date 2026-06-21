# Product Requirements Document
## ArXiv AI Paper Explainer Agent

---

## 1. Problem Statement

AI researchers publish dozens of papers daily on arXiv. Technical practitioners — engineers and developers with ML familiarity but not research-level depth — struggle to keep up. Existing tools either summarize superficially or require reading the full paper. There is no tool that deeply understands a paper's core intent and re-explains it in an intuitive, layered way tailored to practitioners.

---

## 2. Target Users

**Primary:** Technical practitioners — software engineers, ML engineers, and developers who:
- Understand ML fundamentals but don't track research literature daily
- Want to understand *why* a paper matters, not just *what* it says
- Use Obsidian as a personal knowledge management system
- Are comfortable triggering a local Next.js app manually

---

## 3. User Stories

- As a practitioner, I want to trigger the agent on demand so that I can check what new AI papers dropped on arXiv without leaving my workflow.
- As a practitioner, I want to see a curated list of 5 unprocessed papers so that I can quickly decide which one is worth my time.
- As a practitioner, I want to select a paper and receive a rich Markdown explainer so that I understand the paper's core purpose and value without reading the full PDF.
- As a practitioner, I want the explainer saved directly to my Obsidian vault so that it becomes part of my permanent knowledge base.
- As a practitioner, I want follow-up paper suggestions so that I can explore the intellectual lineage of an idea if it interests me.

---

## 4. Functional Requirements

### F1 — Paper Discovery
- On user trigger, agent fetches the latest papers from arXiv's `cs.AI` category via the arXiv API
- Returns the top 5 most recent papers not previously processed

### F2 — Duplicate Detection
- Maintains a local log file (JSON) tracking all processed paper IDs
- Cross-references fetched papers against the log before surfacing recommendations

### F3 — Paper Selection UI
- Displays the 5 candidate papers with title, authors, and abstract snippet
- User selects one paper to process

### F4 — PDF Retrieval
- Agent fetches the full PDF of the selected paper from arXiv

### F5 — Deep Explainer Generation
The generated content must include:
- **Title & metadata** — title, authors, publication date, arXiv ID
- **Problem statement** — what problem the authors are solving and why it matters
- **Core idea** — the central contribution, explained intuitively first then bridged to engineering mental models
- **Methodology** — how they approached the solution, with math handled contextually (translate simple equations; summarize complex proofs at intent level)
- **Key findings** — what they proved or demonstrated
- **Limitations** — what the paper doesn't solve or explicitly acknowledges
- **Why it matters** — real-world implications for practitioners
- **Analogies & intuition** — everyday analogies layered with engineering-anchored explanations
- **Glossary** — top 8–10 must-know terms, prioritized by importance to understanding the paper
- **Follow-up papers** — suggestions drawn from the paper's reference list + agent's training knowledge

### F6 — Obsidian Output
- Saves the explainer as a `.md` file to a dedicated subfolder in the Obsidian vault
- File named consistently: `YYYY-MM-DD_arxiv-id_paper-title-slug.md`
- Updates the processed-papers log file after successful save

### F7 — LLM Configuration
- LLM provider, model name, API key, and parameters are configurable via config file and `.env`
- System is designed to swap providers without code changes

---

## 5. Non-Functional Requirements

- **Local-first** — runs entirely on the user's machine, no cloud infrastructure
- **Low latency expectation** — generation may take 30–120 seconds; acceptable given the task depth
- **Reliability** — if PDF fetch or LLM call fails, surface a clear error; do not silently write a broken note
- **Privacy** — no paper content or user data leaves the machine except to the configured LLM API

---

## 6. Success Metrics

- User can trigger, select, and receive a finished Obsidian note in under 3 minutes (excluding LLM generation time)
- Generated explainer requires no post-editing to be useful and shareable
- Zero duplicate notes generated across multiple runs
- User subjectively rates the explainer as genuinely clarifying the paper's core purpose (not just summarizing it)

---

## 7. Scope & Non-Goals

### In Scope
- arXiv `cs.AI` category only
- Top 5 papers by recency per run
- Single paper processed per trigger
- Local Next.js app (UI + API routes)
- Markdown output to local Obsidian vault
- Configurable LLM provider

### Out of Scope (explicitly)
- Multiple category support *(future)*
- Relevance ranking / keyword filtering *(future)*
- Batch processing multiple papers per run *(future)*
- Cloud hosting or remote access *(future)*
- Obsidian plugin integration *(future)*
- Follow-up paper arXiv fetching / deep linking *(future)*

---

## 8. Open Questions

| ID | Question |
|---|---|
| OQ1 | What is the target Obsidian vault path — hardcoded, or configurable in `.env`? |
| OQ2 | Should the UI display the full abstract or a truncated snippet in the selection step? |
| OQ3 | For follow-up papers sourced from the reference list — should the agent attempt to link arXiv IDs if it can identify them, or list titles only? |
| OQ4 | Is there a maximum note length preference, or should the agent generate as much as needed for full clarity? |
