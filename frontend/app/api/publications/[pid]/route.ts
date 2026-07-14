import { backendBaseURL } from "@/lib/backend";

// PATCH /api/publications/:pid → PATCH {backend}/publications/:pid
// Thin proxy: forwards a partial draft edit (title/content/approve) and
// relays the backend body + status unchanged. In App Router 16, the dynamic
// `params` is a promise and must be awaited.
export async function PATCH(
  request: Request,
  { params }: { params: Promise<{ pid: string }> },
) {
  const { pid } = await params;
  const body = await request.json();
  try {
    const res = await fetch(
      `${backendBaseURL()}/publications/${encodeURIComponent(pid)}`,
      {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      },
    );
    const text = await res.text();
    return new Response(text, {
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
