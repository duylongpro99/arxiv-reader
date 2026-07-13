import { backendBaseURL } from "@/lib/backend";

// GET /api/runs/:id/content → GET {backend}/runs/:id/content
// History-content proxy: relays the persisted note's markdown. `available:false`
// (vault file moved/deleted) is still a 200 from the backend; 503 means history
// itself is unavailable (no DB configured). Relays the backend body + status
// unchanged. In App Router 16, the dynamic `params` is a promise and must be
// awaited.
export async function GET(
  _request: Request,
  { params }: { params: Promise<{ id: string }> },
) {
  const { id } = await params;
  try {
    const res = await fetch(
      `${backendBaseURL()}/runs/${encodeURIComponent(id)}/content`,
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
