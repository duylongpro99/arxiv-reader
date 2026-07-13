import { backendBaseURL } from "@/lib/backend";

// POST /api/discover/:sessionId/more → POST {backend}/discover/:sessionId/more
// Load-more proxy: fetches the next page of candidate papers for an existing
// discovery session. Synchronous — the backend returns the page directly in
// the response, no polling. Relays 404 (expired/unknown session) unchanged. In
// App Router 16, the dynamic `params` is a promise and must be awaited.
export async function POST(
  _request: Request,
  { params }: { params: Promise<{ sessionId: string }> },
) {
  const { sessionId } = await params;
  try {
    const res = await fetch(
      `${backendBaseURL()}/discover/${encodeURIComponent(sessionId)}/more`,
      { method: "POST" },
    );
    const body = await res.text();
    return new Response(body, {
      status: res.status,
      headers: { "Content-Type": "application/json" },
    });
  } catch {
    return Response.json(
      { error: "Cannot reach the discovery service. Is the backend running?" },
      { status: 502 },
    );
  }
}
