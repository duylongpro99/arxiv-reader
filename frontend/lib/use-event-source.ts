"use client";

import { useEffect, useState } from "react";
import { publicBackendURL } from "./backend-public";
import type { TimelineEvent } from "./types";

// isStreamTerminal reports whether an event ends the live stream: a completion,
// or a NON-recoverable failure. A recoverable failure is NOT terminal — a retry
// resumes the same run and keeps emitting on the open stream (mirrors the
// backend recorder lifecycle), so we keep listening.
function isStreamTerminal(evt: TimelineEvent): boolean {
  if (evt.type === "run.completed") return true;
  if (evt.type === "run.failed") return evt.summary?.recoverable === false;
  return false;
}

export interface EventSourceState {
  events: TimelineEvent[];
  done: boolean; // stream reached a true terminal
  error: boolean; // connection error (backend down / CORS)
}

// State bundles the accumulator with the runId it belongs to, so a run change
// resets cleanly (see the render-time reset below) and a stale callback from a
// closing EventSource can be ignored.
interface State extends EventSourceState {
  runId: string | null;
}

// useEventSource opens an SSE connection to the backend's live run timeline and
// accumulates events into a seq-ordered, deduplicated array. It closes the
// source on a terminal event and on unmount. A null runId is a no-op. The
// connection is direct-to-backend (see publicBackendURL).
export function useEventSource(runId: string | null): EventSourceState {
  const [state, setState] = useState<State>({ runId, events: [], done: false, error: false });

  // Reset when the run changes. Adjusting state during render (guarded, so it
  // can't loop) is React's documented pattern for "derive fresh state from a
  // changed prop" — cleaner than a setState-in-effect (which cascades renders).
  if (state.runId !== runId) {
    setState({ runId, events: [], done: false, error: false });
  }

  useEffect(() => {
    if (!runId) return;
    const url = `${publicBackendURL()}/runs/${encodeURIComponent(runId)}/events`;
    const es = new EventSource(url);

    // Single handler: frames are default (unnamed) SSE messages; the kind lives
    // in data.type. Merge by seq so replay-on-reconnect never duplicates a row.
    // A successful (re)connection clears any prior transient-error flag, so a
    // brief network blip doesn't leave the "reconnecting" banner stuck on for the
    // rest of an otherwise-healthy run.
    es.onopen = () => {
      setState((prev) => (prev.runId === runId && prev.error ? { ...prev, error: false } : prev));
    };

    es.onmessage = (e: MessageEvent<string>) => {
      let evt: TimelineEvent;
      try {
        evt = JSON.parse(e.data) as TimelineEvent;
      } catch {
        return; // ignore a malformed frame rather than break the stream
      }
      setState((prev) => {
        if (prev.runId !== runId) return prev; // stale callback from an old run
        return {
          ...prev,
          events: mergeBySeq(prev.events, evt),
          done: prev.done || isStreamTerminal(evt),
          error: false, // receiving data means the connection is healthy again
        };
      });
      if (isStreamTerminal(evt)) es.close(); // stop auto-reconnect on a finished run
    };

    es.onerror = () => {
      setState((prev) => (prev.runId === runId ? { ...prev, error: true } : prev));
    };

    return () => es.close();
  }, [runId]);

  return { events: state.events, done: state.done, error: state.error };
}

// mergeBySeq inserts evt into the ascending-seq array, replacing an existing
// entry with the same seq (idempotent under replay+live overlap).
function mergeBySeq(prev: TimelineEvent[], evt: TimelineEvent): TimelineEvent[] {
  const idx = prev.findIndex((e) => e.seq === evt.seq);
  if (idx !== -1) {
    const next = prev.slice();
    next[idx] = evt;
    return next;
  }
  // Fast path: strictly-increasing seq (the common live case) appends.
  if (prev.length === 0 || evt.seq > prev[prev.length - 1].seq) {
    return [...prev, evt];
  }
  // Out-of-order arrival: insert in sorted position.
  const next = [...prev, evt];
  next.sort((a, b) => a.seq - b.seq);
  return next;
}
