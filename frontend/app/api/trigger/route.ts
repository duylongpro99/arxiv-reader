import { backendBaseURL } from "@/lib/backend";

// POST /api/trigger → POST {backend}/discover
// Thin proxy: starts a discovery run and relays the { session_id } response.
export async function POST() {
  try {
    const res = await fetch(`${backendBaseURL()}/discover`, { method: "POST" });
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
