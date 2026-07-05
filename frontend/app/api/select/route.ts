import { backendBaseURL } from "@/lib/backend";

// POST /api/select → POST {backend}/process
// Thin proxy: forwards the chosen { session_id, paper_id } and relays the
// backend's JSON response (including its 4xx validation errors) unchanged. The
// backend base URL is read server-side only, never bundled into client JS.
export async function POST(request: Request) {
  const { session_id, paper_id } = await request.json();
  try {
    const res = await fetch(`${backendBaseURL()}/process`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ session_id, paper_id }),
    });
    const body = await res.text();
    return new Response(body, {
      status: res.status,
      headers: { "Content-Type": "application/json" },
    });
  } catch {
    return Response.json(
      { error: "Cannot reach the service. Is the backend running?" },
      { status: 502 },
    );
  }
}
