import { backendBaseURL } from "@/lib/backend";

// GET /api/categories → GET {backend}/categories
// Thin proxy for the cs.* catalog that populates the discovery category picker.
// Relays the backend body + status unchanged.
export async function GET() {
  try {
    const res = await fetch(`${backendBaseURL()}/categories`);
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
