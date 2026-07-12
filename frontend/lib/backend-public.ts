// Client-visible backend base URL, used ONLY for the SSE EventSource connection,
// which must hit the Go backend directly (proxying SSE through Next.js risks
// buffering the stream). REST calls still go through the same-origin /api/*
// proxies. NEXT_PUBLIC_ is inlined into the client bundle at build time; the
// default matches the loopback address the backend binds.
//
// CORS: the backend allows the http://localhost:3000 dev origin (see the Go
// corsMiddleware), so the browser can open this cross-origin EventSource.
export function publicBackendURL(): string {
  return process.env.NEXT_PUBLIC_BACKEND_URL ?? "http://127.0.0.1:8080";
}
