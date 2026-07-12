package orchestrator

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// This file holds the Server-Sent Events plumbing for the live run timeline,
// kept separate from the handler logic in runs-handlers.go.

// setSSEHeaders writes the standard event-stream headers. Cache/keep-alive/
// X-Accel-Buffering disable any intermediary buffering so frames arrive live.
func setSSEHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
}

// writeEventDTO writes one event (in the shared wire shape) as an SSE frame and
// flushes it. The `id:` line is the seq (EventSource echoes it back as
// Last-Event-ID on reconnect → free resume); `data:` is the EventDTO JSON on a
// single line (json.Marshal never emits a bare newline). We deliberately DON'T
// set a named `event:` field: the browser's native EventSource fires `onmessage`
// only for default (unnamed) events, and the client consumes the whole
// heterogeneous timeline with a single handler, reading the kind from data.type.
// A write/flush error (client gone) is returned so the caller can stop.
func writeEventDTO(w http.ResponseWriter, rc *http.ResponseController, dto EventDTO) error {
	data, err := json.Marshal(dto)
	if err != nil {
		return nil // skip an unmarshalable event rather than kill the stream
	}
	if _, err := w.Write(sseFrame(dto.Seq, data)); err != nil {
		return err
	}
	return rc.Flush()
}

// sseFrame assembles the id/data lines for one message.
func sseFrame(seq int, data []byte) []byte {
	b := make([]byte, 0, len(data)+32)
	b = append(b, "id: "...)
	b = strconv.AppendInt(b, int64(seq), 10)
	b = append(b, '\n')
	b = append(b, "data: "...)
	b = append(b, data...)
	b = append(b, '\n', '\n')
	return b
}

// parseSinceSeq resolves the resume point: the standard Last-Event-ID request
// header (set automatically by EventSource on reconnect), or a ?lastEventId=
// query fallback (handy for curl). Returns -1 (replay everything) when absent or
// malformed — seq values are >= 0, so "> -1" means "all".
func parseSinceSeq(r *http.Request) int {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("lastEventId")
	}
	if raw == "" {
		return -1
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return -1
	}
	return n
}
