import { backendBaseURL } from "@/lib/backend";

// GET /api/channels → GET {backend}/channels
// Thin proxy: lists every enabled, resolvable publish channel. Relays the
// backend body + status unchanged (never 503s itself — the channel list
// doesn't require a DB — but forwards whatever the backend returns).
export async function GET() {
  try {
    const res = await fetch(`${backendBaseURL()}/channels`);
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
