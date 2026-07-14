import { backendBaseURL } from "@/lib/backend";

// POST /api/publications/:pid/publish → POST {backend}/publications/:pid/publish
// Thin proxy: pushes an approved draft live. Relays the backend body + status
// unchanged, including 409 (already published) and 502 (channel error). In
// App Router 16, the dynamic `params` is a promise and must be awaited.
export async function POST(
  _request: Request,
  { params }: { params: Promise<{ pid: string }> },
) {
  const { pid } = await params;
  try {
    const res = await fetch(
      `${backendBaseURL()}/publications/${encodeURIComponent(pid)}/publish`,
      { method: "POST" },
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
