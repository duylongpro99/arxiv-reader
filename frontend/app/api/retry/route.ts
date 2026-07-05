import { backendBaseURL } from "@/lib/backend";

// POST /api/retry → POST {backend}/retry/{session_id}
// Thin proxy for the resume-from-failed-stage endpoint (F2). Forwards the
// { session_id } and relays the backend's JSON (including its 400 "not
// retryable" / 404 "session not found") unchanged. Backend URL stays server-side.
export async function POST(request: Request) {
  const { session_id } = await request.json();
  if (!session_id) {
    return Response.json({ error: "session_id is required" }, { status: 400 });
  }
  try {
    const res = await fetch(
      `${backendBaseURL()}/retry/${encodeURIComponent(session_id)}`,
      { method: "POST" },
    );
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
