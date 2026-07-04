// Server-only helper: resolves the Go backend base URL. Read exclusively inside
// route handlers so the address is never bundled into client JS. Falls back to
// the local loopback address the Go server binds (127.0.0.1:8080).
export function backendBaseURL(): string {
  return process.env.BACKEND_URL ?? "http://127.0.0.1:8080";
}
