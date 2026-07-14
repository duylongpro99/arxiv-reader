package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/agents/repurposer"
	"github.com/maritime-ds/arxiv-reader/internal/channels"
	"github.com/maritime-ds/arxiv-reader/internal/models"
	"github.com/maritime-ds/arxiv-reader/internal/store"
	"github.com/maritime-ds/arxiv-reader/internal/tracing"
)

// This file holds the Phase 10 channel-publishing endpoints: list channels,
// generate per-channel drafts (one agent call per unique category), list/edit/
// approve, and publish. Every publishing endpoint is DB-required — publishing
// state (idempotency, retry) must be durable, so a nil publications store
// degrades every handler here to 503 while leaving the rest of the app
// (discovery/pipeline/history) completely untouched.

// ContentRepurposer is the narrow, consumer-side contract for the category-
// blind content generator. Declared here (not in agents/repurposer) so the
// orchestrator stays testable with a fake, mirroring Explainer/Reviewer.
type ContentRepurposer interface {
	Generate(ctx context.Context, in repurposer.RepurposeInput) (channels.GeneratedContent, error)
}

// PublicationStore is the write+read contract for publishing. *store.Store
// satisfies it. It also carries ListEvents + AppendEvent (already used by the
// history/tracing read path) because publication timeline events are written
// onto a run whose live recorder is gone — see emitPublicationEvent below.
type PublicationStore interface {
	CreatePublication(ctx context.Context, p store.PublicationRecord) (bool, error)
	ListPublicationsByRun(ctx context.Context, runID string) ([]store.PublicationRecord, error)
	GetPublication(ctx context.Context, id string) (store.PublicationRecord, error)
	UpdatePublicationContent(ctx context.Context, id, title, content, status string) error
	MarkPublished(ctx context.Context, id, url, extID string) error
	MarkFailed(ctx context.Context, id, errMsg string) error
	ClaimForPublish(ctx context.Context, id string) (bool, error)
	ListEvents(ctx context.Context, runID string, sinceSeq int) ([]store.EventRecord, error)
	AppendEvent(ctx context.Context, e store.EventRecord) error
}

// publishTimeout bounds a full publish action. Unlike a pure DB read, publishing
// does slow network work — the X channel refreshes an OAuth token then posts a
// multi-tweet thread sequentially, and dev.to may retry a 429 — so it needs a
// generous budget well beyond dbReadTimeout (which is for DB reads only).
const publishTimeout = 3 * time.Minute

// generateTimeout returns the budget for a draft-generation request. It is sized
// to the LLM request budget (a longform draft is a full LLM completion, ~tens of
// seconds), NOT the 5s dbReadTimeout — reusing that here would cancel every real
// generation at 5s. Falls back to 120s if the LLM timeout is unset.
func (o *Orchestrator) generateTimeout() time.Duration {
	if o.cfg.LLM.RequestTimeoutSec > 0 {
		return time.Duration(o.cfg.LLM.RequestTimeoutSec) * time.Second
	}
	return 120 * time.Second
}

// publishingUnavailable is the standard 503 body every publishing handler
// returns when o.publications is nil (no DSN, or the DB is unreachable).
func publishingUnavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "publishing requires the database"})
}

// HandleChannels lists every configured channel that resolves successfully.
// A channel that errors to construct (missing API key, not yet implemented —
// see channels.NewChannel's default case) is skipped rather than failing the
// whole list: the operator sees the channels that ARE usable today.
func (o *Orchestrator) HandleChannels(w http.ResponseWriter, r *http.Request) {
	dtos := make([]ChannelDTO, 0, len(o.cfg.Publishing.Channels))
	for _, id := range o.cfg.Publishing.Channels {
		ch, err := o.channelFactory(id, o.cfg)
		if err != nil {
			slog.Debug("channel unavailable", "channel_id", id, "reason", err.Error())
			continue
		}
		dtos = append(dtos, ChannelDTO{ID: ch.ID(), Category: string(ch.Category())})
	}
	writeJSON(w, http.StatusOK, ChannelsResponse{Channels: dtos})
}

// HandleCreatePublications generates a draft Publication per requested
// channel: one Repurposer.Generate call per UNIQUE category (dedup — two
// channels sharing a category cost a single LLM call), then a fan-out
// CreatePublication row per channel. Re-requesting an existing (run, channel)
// pair is idempotent: the existing row is returned as-is, never re-generated.
func (o *Orchestrator) HandleCreatePublications(w http.ResponseWriter, r *http.Request) {
	if o.publications == nil {
		publishingUnavailable(w)
		return
	}
	runID := r.PathValue("id")

	var req CreatePublicationsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Channels) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// LLM-budget timeout: the per-category Generate calls below are full LLM
	// completions, not DB reads — dbReadTimeout (5s) would cancel them all.
	ctx, cancel := context.WithTimeout(r.Context(), o.generateTimeout())
	defer cancel()

	markdown, err := o.readRunMarkdown(ctx, runID)
	if err != nil {
		writeJSON(w, err.status, map[string]string{"error": err.message})
		return
	}

	// The run header carries the paper metadata the Repurposer rides through
	// onto GeneratedContent (title/id for attribution); o.store is the same
	// RunReader assigned alongside o.publications, so it is non-nil here too.
	run, rerr := o.store.GetRun(ctx, runID)
	if rerr != nil {
		if errors.Is(rerr, store.ErrRunNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read run"})
		return
	}
	paper := models.Paper{ID: derefOr(run.PaperID), Title: derefOr(run.PaperTitle)}

	// Resolve each requested channel id up front; an unresolvable id (not
	// configured, or not yet implemented) is skipped and reported back rather
	// than failing the whole batch — the caller may have requested a mix of
	// working and not-yet-shipped channels.
	type resolvedChannel struct {
		id string
		ch channels.Channel
	}
	resolved := make([]resolvedChannel, 0, len(req.Channels))
	var skipped []string
	for _, id := range req.Channels {
		ch, cerr := o.channelFactory(id, o.cfg)
		if cerr != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %s", id, cerr.Error()))
			continue
		}
		resolved = append(resolved, resolvedChannel{id: id, ch: ch})
	}

	// Generate per UNIQUE category — the whole point of the category/channel
	// split (design note §3): two channels sharing a category cost one LLM call.
	generated := make(map[channels.Category]channels.GeneratedContent, len(resolved))
	for _, rc := range resolved {
		cat := rc.ch.Category()
		if _, done := generated[cat]; done {
			continue
		}
		gc, gerr := o.repurpose.Generate(ctx, repurposer.RepurposeInput{Raw: markdown, Category: cat, PaperMeta: paper})
		if gerr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "content generation failed: " + scrubErr(gerr.Error())})
			return
		}
		generated[cat] = gc
	}

	// Fan out: one editable Publication draft per requested channel, all
	// sharing the per-category GeneratedContent computed above. Existing rows
	// for this run are read ONCE up front (not per-conflict) so the idempotent
	// branch below is an O(1) map lookup rather than an N+1 query per channel.
	priorByChannel := make(map[string]store.PublicationRecord)
	if prior, lerr := o.publications.ListPublicationsByRun(ctx, runID); lerr == nil {
		for _, p := range prior {
			priorByChannel[p.ChannelID] = p
		}
	}
	dtos := make([]PublicationDTO, 0, len(resolved))
	for _, rc := range resolved {
		gc := generated[rc.ch.Category()]
		title := gc.Title
		rec := store.PublicationRecord{
			ID: newSessionID(), RunID: runID, ChannelID: rc.id,
			Category: string(rc.ch.Category()), Status: "draft",
			AdaptedContent: gc.Body, Title: &title, CreatedAt: time.Now(),
		}
		inserted, cerr := o.publications.CreatePublication(ctx, rec)
		if cerr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot create publication"})
			return
		}
		if !inserted {
			// (run_id, channel_id) already exists — idempotent: return the
			// existing row untouched rather than silently re-drafting it.
			if e, ok := priorByChannel[rc.id]; ok {
				dtos = append(dtos, publicationDTOFromRecord(e))
			}
			continue
		}
		dtos = append(dtos, publicationDTOFromRecord(rec))
		o.emitPublicationEvent(ctx, runID, tracing.KindPublicationDraftGenerated, tracing.StatusSuccess,
			fmt.Sprintf("Draft generated for %s (%s)", rc.id, rc.ch.Category()),
			map[string]any{"channelId": rc.id, "category": string(rc.ch.Category()), "titlePreview": preview(title, 100)})
	}

	writeJSON(w, http.StatusOK, PublicationsResponse{Publications: dtos, SkippedChannels: skipped})
}

// HandleListPublications lists every draft/attempt for a run, oldest first.
func (o *Orchestrator) HandleListPublications(w http.ResponseWriter, r *http.Request) {
	if o.publications == nil {
		publishingUnavailable(w)
		return
	}
	runID := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), dbReadTimeout)
	defer cancel()

	recs, err := o.publications.ListPublicationsByRun(ctx, runID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read publications"})
		return
	}
	dtos := make([]PublicationDTO, 0, len(recs))
	for _, p := range recs {
		dtos = append(dtos, publicationDTOFromRecord(p))
	}
	writeJSON(w, http.StatusOK, PublicationsResponse{Publications: dtos})
}

// HandlePatchPublication applies a human edit and/or approval to a draft.
// Editing an already-published row is rejected (409) — a live post is
// immutable; the row stays as the durable record of what actually went out.
//
// Approval semantics: approve:true sets status to "approved"; any other edit
// (title/content only, or approve:false) resets status to "draft". This is
// intentional, not accidental — an edit after approval must be re-reviewed
// before it can be published, so approval never silently survives a content
// change made in the same or a later PATCH.
func (o *Orchestrator) HandlePatchPublication(w http.ResponseWriter, r *http.Request) {
	if o.publications == nil {
		publishingUnavailable(w)
		return
	}
	pid := r.PathValue("pid")

	var req PatchPublicationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbReadTimeout)
	defer cancel()

	pub, err := o.publications.GetPublication(ctx, pid)
	if err != nil {
		if errors.Is(err, store.ErrPublicationNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "publication not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read publication"})
		return
	}
	if pub.Status == "published" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "cannot edit a published publication"})
		return
	}

	title := derefOr(pub.Title)
	if req.Title != nil {
		title = *req.Title
	}
	content := pub.AdaptedContent
	if req.Content != nil {
		content = *req.Content
	}
	status := "draft"
	if req.Approve != nil && *req.Approve {
		status = "approved"
	}

	if err := o.publications.UpdatePublicationContent(ctx, pid, title, content, status); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot update publication"})
		return
	}
	pub.Title = &title
	pub.AdaptedContent = content
	pub.Status = status
	writeJSON(w, http.StatusOK, publicationDTOFromRecord(pub))
}

// HandlePublish validates and pushes a publication live. Already-published
// rows 409 (idempotent — no re-post of a live URL, ever). A channel/validation
// failure is recorded via MarkFailed (retryable: the row stays in place) and
// returned as 502/422 with a scrubbed message; a success stores the external
// URL/ID via MarkPublished. Both outcomes emit a best-effort timeline event.
func (o *Orchestrator) HandlePublish(w http.ResponseWriter, r *http.Request) {
	if o.publications == nil {
		publishingUnavailable(w)
		return
	}
	pid := r.PathValue("pid")
	// Publish budget (token refresh + a multi-tweet thread), NOT the 5s DB-read
	// timeout — that would cancel a real thread mid-post.
	ctx, cancel := context.WithTimeout(r.Context(), publishTimeout)
	defer cancel()

	pub, err := o.publications.GetPublication(ctx, pid)
	if err != nil {
		if errors.Is(err, store.ErrPublicationNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "publication not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read publication"})
		return
	}
	// Friendly, specific rejections for the two non-claimable terminal/initial
	// states (the atomic ClaimForPublish below is the REAL race guard; these
	// only produce a clearer message than a generic "not publishable"):
	//   - published: never re-post a live URL (idempotency, design #1 hazard).
	//   - draft: the human-review gate — approval is required before publishing,
	//     enforced server-side, not only in the UI (the API is the trust boundary).
	switch pub.Status {
	case "published":
		writeJSON(w, http.StatusConflict, map[string]string{"error": "publication already published"})
		return
	case "draft":
		writeJSON(w, http.StatusConflict, map[string]string{"error": "publication must be approved before publishing"})
		return
	}

	run, rerr := o.store.GetRun(ctx, pub.RunID)
	if rerr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read run"})
		return
	}
	gc := channels.GeneratedContent{
		Category:  channels.Category(pub.Category),
		Title:     derefOr(pub.Title),
		Body:      pub.AdaptedContent,
		PaperMeta: models.Paper{ID: derefOr(run.PaperID), Title: derefOr(run.PaperTitle)},
	}

	ch, cerr := o.channelFactory(pub.ChannelID, o.cfg)
	if cerr != nil {
		// The channel id was resolvable enough to create the draft earlier, but
		// config can drift (e.g. an API key removed) between draft and publish.
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "channel unavailable: " + scrubErr(cerr.Error())})
		return
	}
	if verr := ch.Validate(gc); verr != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": scrubErr(verr.Error())})
		return
	}

	// Atomically claim the row (approved|failed → publishing) IMMEDIATELY before
	// the irreversible post. This is the guard against a concurrent double-publish
	// (the design's #1 hazard): only one racing request wins the claim; a lost
	// claim means the row was already published or is being published right now,
	// so we refuse rather than post twice. Placed after Validate so a validation
	// failure leaves the row publishable (no wasted claim).
	claimed, clErr := o.publications.ClaimForPublish(ctx, pid)
	if clErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot claim publication for publishing"})
		return
	}
	if !claimed {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "publication is not in a publishable state"})
		return
	}

	result, perr := ch.Publish(ctx, gc)
	if perr != nil {
		msg := scrubErr(perr.Error())
		_ = o.publications.MarkFailed(ctx, pid, msg) // best-effort — the publish failure itself is the priority to surface
		o.emitPublicationEvent(ctx, pub.RunID, tracing.KindPublicationFailed, tracing.StatusError,
			fmt.Sprintf("Publish failed on %s", pub.ChannelID),
			map[string]any{"channelId": pub.ChannelID, "category": pub.Category, "error": msg})
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": msg})
		return
	}
	if err := o.publications.MarkPublished(ctx, pid, result.ExternalURL, result.ExternalID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot record publish result"})
		return
	}
	o.emitPublicationEvent(ctx, pub.RunID, tracing.KindPublicationPublished, tracing.StatusSuccess,
		fmt.Sprintf("Published to %s", pub.ChannelID),
		map[string]any{"channelId": pub.ChannelID, "category": pub.Category, "url": result.ExternalURL, "titlePreview": preview(gc.Title, 100)})

	now := time.Now()
	pub.Status = "published"
	pub.ExternalURL = &result.ExternalURL
	pub.ExternalID = &result.ExternalID
	pub.PublishedAt = &now
	writeJSON(w, http.StatusOK, publicationDTOFromRecord(pub))
}

// emitPublicationEvent best-effort persists one publication timeline entry
// directly via the store. Publishing targets COMPLETED runs whose live
// recorder is gone (evicted after the run finished), so o.tracer/NewRecorder
// cannot be used here: a fresh Recorder's seq counter restarts at 0, which
// collides with the run_events primary key (run_id, seq) already written by
// the original pipeline run. Instead this reads the run's current max seq and
// appends the next one directly — additive, never fatal to the publish action
// itself (errors are swallowed; a missing event is strictly worse UX, never a
// broken publish).
//
// Summaries are metadata-only (channel id, category, url/title preview) by
// contract — this path bypasses the Recorder's scrubber, so no secret or full
// body may ever be placed in the summary map passed here.
func (o *Orchestrator) emitPublicationEvent(ctx context.Context, runID string, kind tracing.EventKind, status tracing.Status, title string, summary map[string]any) {
	if o.publications == nil {
		return
	}
	events, err := o.publications.ListEvents(ctx, runID, -1)
	if err != nil {
		return // best-effort
	}
	nextSeq := 0
	for _, e := range events {
		if e.Seq >= nextSeq {
			nextSeq = e.Seq + 1
		}
	}
	b, _ := json.Marshal(summary)
	_ = o.publications.AppendEvent(ctx, store.EventRecord{
		RunID: runID, Seq: nextSeq, EventType: string(kind), Stage: "publishing",
		Title: title, Status: string(status), Summary: b, CreatedAt: time.Now(),
	})
}
