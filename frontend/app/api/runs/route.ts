import type { NextRequest } from "next/server";
import { backendBaseURL } from "@/lib/backend";

// GET /api/runs?limit=&offset= → GET {backend}/runs?…
// History list proxy. Relays the backend body + status unchanged (including its
// 503 when Postgres is not configured). The live SSE stream is NOT proxied — the
// browser's EventSource connects to the backend directly (see lib/backend-public).
export async function GET(request: NextRequest) {
  const qs = request.nextUrl.search; // preserves ?limit/&offset
  try {
    const res = await fetch(`${backendBaseURL()}/runs${qs}`);
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
