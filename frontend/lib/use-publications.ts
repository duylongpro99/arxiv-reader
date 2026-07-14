"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  generateDrafts,
  getChannels,
  getRunPublications,
  patchPublication,
  publishPublication,
} from "./api";
import type {
  ChannelsResponse,
  PatchPublicationRequest,
  Publication,
  PublicationsResponse,
} from "./types";
import { HistoryUnavailableError } from "./use-runs";

const UNAVAILABLE_MESSAGE =
  "Publishing is unavailable — start Postgres and set DATABASE_URL.";

// rethrow503 turns the shared "no DB configured" 503 (the same condition
// HistoryUnavailableError already models for run history) into that error so
// publish-ui components can show one consistent "unavailable" hint instead of
// a generic error banner. Always throws — never returns.
function rethrow503(err: unknown): never {
  const status = (err as { status?: number }).status;
  if (status === 503) throw new HistoryUnavailableError(UNAVAILABLE_MESSAGE);
  throw err;
}

// useChannels lists every enabled, resolvable publish channel via the
// /api/channels proxy.
export function useChannels() {
  return useQuery<ChannelsResponse>({
    queryKey: ["channels"],
    queryFn: async () => {
      try {
        return await getChannels();
      } catch (err) {
        rethrow503(err);
      }
    },
  });
}

// useRunPublications lists the publish drafts/attempts already created for a
// run. Disabled until a run id is provided.
export function useRunPublications(runId: string | null) {
  return useQuery<PublicationsResponse>({
    queryKey: ["publications", runId],
    enabled: !!runId,
    queryFn: async () => {
      try {
        return await getRunPublications(runId as string);
      } catch (err) {
        rethrow503(err);
      }
    },
  });
}

// useGenerateDrafts creates one draft publication per requested channel id for
// a run, then refreshes the run's publication list.
export function useGenerateDrafts(runId: string) {
  const queryClient = useQueryClient();
  return useMutation<PublicationsResponse, Error, string[]>({
    mutationFn: async (channels) => {
      try {
        return await generateDrafts(runId, channels);
      } catch (err) {
        rethrow503(err); // map "no DB" 503 to the same unavailable hint
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["publications", runId] });
    },
  });
}

// usePatchPublication applies a partial edit (title/content) and/or approves a
// draft. `runId` is required up front (rather than read off the response) so
// the publications list can be re-synced even on a failed request.
export function usePatchPublication(runId: string) {
  const queryClient = useQueryClient();
  return useMutation<
    Publication,
    Error,
    { pid: string; body: PatchPublicationRequest }
  >({
    mutationFn: ({ pid, body }) => patchPublication(pid, body),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["publications", runId] });
    },
  });
}

// usePublishPublication sends an approved draft to its channel. Invalidates on
// settle (not just success): a failed publish attempt still records the
// scrubbed error + "failed" status server-side (MarkFailed), so the draft
// card must refetch to show that outcome too.
export function usePublishPublication(runId: string) {
  const queryClient = useQueryClient();
  return useMutation<Publication, Error, string>({
    mutationFn: (pid) => publishPublication(pid),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["publications", runId] });
    },
  });
}
