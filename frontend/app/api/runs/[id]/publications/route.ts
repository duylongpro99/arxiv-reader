import { backendBaseURL } from "@/lib/backend";

// GET  /api/runs/:id/publications  → GET  {backend}/runs/:id/publications
// POST /api/runs/:id/publications  → POST {backend}/runs/:id/publications
// Publications proxy: lists a run's drafts (GET) or generates new ones for the
// requested channels (POST, forwards the `{channels}` body). Relays the
// backend body + status unchanged, including its 503 when publishing is
// unavailable (no DB configured). In App Router 16, the dynamic `params` is a
// promise and must be awaited.
export async function GET(
  _request: Request,
  { params }: { params: Promise<{ id: string }> },
) {
  const { id } = await params;
  try {
    const res = await fetch(
      `${backendBaseURL()}/runs/${encodeURIComponent(id)}/publications`,
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

export async function POST(
  request: Request,
  { params }: { params: Promise<{ id: string }> },
) {
  const { id } = await params;
  const { channels } = await request.json();
  try {
    const res = await fetch(
      `${backendBaseURL()}/runs/${encodeURIComponent(id)}/publications`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ channels }),
      },
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
