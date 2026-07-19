import { backendBaseURL } from "@/lib/backend";

// GET /api/resources → GET {backend}/resources
// Thin proxy for the resource descriptors that drive the resource picker + the
// dynamic request form. Relays the backend body + status unchanged.
export async function GET() {
  try {
    const res = await fetch(`${backendBaseURL()}/resources`);
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
