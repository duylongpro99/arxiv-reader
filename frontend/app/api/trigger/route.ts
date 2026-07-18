import type { NextRequest } from "next/server";
import { backendBaseURL } from "@/lib/backend";

// POST /api/trigger → POST {backend}/discover
// Thin proxy: starts a discovery run and relays the { session_id } response.
// Forwards the optional { category, terms } body so the user's picker selection
// reaches the backend. An empty body is fine — the backend falls back to its
// configured default category.
export async function POST(request: NextRequest) {
  // Read the incoming body defensively: an empty body yields "" which we forward
  // as "{}"; the backend treats an empty object as "use defaults".
  const body = await request.text();
  try {
    const res = await fetch(`${backendBaseURL()}/discover`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: body || "{}",
    });
    const respBody = await res.text();
    return new Response(respBody, {
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
