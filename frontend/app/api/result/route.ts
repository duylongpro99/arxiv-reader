import type { NextRequest } from "next/server";
import { backendBaseURL } from "@/lib/backend";

// GET /api/result?sessionId=xxx → GET {backend}/result/{sessionId}
// Relays the backend response (including its 404 "result not ready" body)
// unchanged. Mirrors app/api/status/route.ts — the backend base URL stays
// server-side so it is never bundled into client JS.
export async function GET(request: NextRequest) {
  const sessionId = request.nextUrl.searchParams.get("sessionId");
  if (!sessionId) {
    return Response.json({ error: "sessionId is required" }, { status: 400 });
  }

  try {
    const res = await fetch(
      `${backendBaseURL()}/result/${encodeURIComponent(sessionId)}`,
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
