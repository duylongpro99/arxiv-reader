export default function Home() {
  return (
    <main style={{ padding: 32, fontFamily: "system-ui", lineHeight: 1.6 }}>
      <h1>ArXiv AI Paper Explainer — Dev Shell</h1>
      <p>Frontend is running on http://localhost:3000</p>
      <p>
        Backend health check:{" "}
        <a href="http://127.0.0.1:8080/health">http://127.0.0.1:8080/health</a>
      </p>
      <p>Phase 1 scaffold. No UI yet.</p>
    </main>
  );
}
