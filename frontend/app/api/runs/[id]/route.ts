import { backendBaseURL } from "@/lib/backend";

// GET /api/runs/:id → GET {backend}/runs/:id
// Reopen-a-run proxy (header + full persisted timeline). Relays the backend body
// + status unchanged (404 unknown run, 503 when history is unavailable). In App
// Router 16, the dynamic `params` is a promise and must be awaited.
export async function GET(
  _request: Request,
  { params }: { params: Promise<{ id: string }> },
) {
  const { id } = await params;
  try {
    const res = await fetch(`${backendBaseURL()}/runs/${encodeURIComponent(id)}`);
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
