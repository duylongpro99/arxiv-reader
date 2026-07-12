# Design Note — Frontend Redesign: "Dark Instrument + Reader"

Problem: The frontend ships with zero visual identity (system Arial, default
Tailwind blue, ad-hoc `dark:` pairs on every element). It needs a cohesive design
that (a) serves a technical practitioner running a local tool and (b) makes the
long-form paper explainer genuinely pleasant to read — two different moods in one
app. Light and dark must be true equals (system-driven).

Structure: One token system, two visual *zones*.

- **Semantic token layer** (`app/globals.css`): all color decisions live in CSS
  custom properties that switch under `prefers-color-scheme`, exposed to Tailwind
  via `@theme inline`. Components reference semantic utilities (`bg-surface`,
  `text-ink`, `text-muted`, `border-line`, `text-accent`, `bg-accent-solid`,
  `text-ok/warn/err/info/tool`) — never raw hex, never hand-written `dark:` pairs.
  This removes ~90% of the `dark:` duplication and makes theming single-sourced.

- **Instrument shell** (discovery, progress, timeline, runs): compact, mono-flavored,
  hairline borders + faint elevation, terminal-precise. The live run timeline is a
  signature-quality but *supporting* side element (user's "1 support for 2" choice):
  colored status glyphs, mono timestamps/durations, a "running" pulse on the live
  stream, tasteful row entrance.

- **Reader surface** (the explainer markdown): calm, high-contrast, generous measure
  (~68ch), sans body at 17px with monospace accents for code / arXiv IDs / metadata.

Type: Geist Sans (UI + reading) + Geist Mono (meta, logs, IDs, figures). Tabular
mono figures for durations, token counts, and `$` costs so numbers don't jitter.

Motion: 150–250ms ease-out; subtle fade-in on timeline rows; one accent glow on the
active running row only. `prefers-reduced-motion` disables all of it. Global
`:focus-visible` ring in the accent color. Icons are inline SVG (local
`components/icons.tsx`, stroke 1.5) — no emoji, no new dependency.

Tradeoffs considered and rejected:
- *Adding lucide-react*: rejected — a whole dep for ~8 glyphs; local SVG is leaner
  (YAGNI) and sidesteps npm sandbox friction.
- *A manual theme toggle*: rejected for v1 — user chose system-driven equality;
  `prefers-color-scheme` needs no toggle, no persisted state, no hydration risk.
- *@tailwindcss/typography for the reader*: rejected — the explicit react-markdown
  component map already exists and keeps the reader on our own tokens; adding `prose`
  would fight the token system.
- *Keeping per-element `dark:` pairs*: rejected — the token layer is the elegant
  single source of truth and shrinks every component.

Scope: `globals.css`, `layout.tsx`, `page.tsx`, `runs` pages, all 12 components,
plus a new `components/icons.tsx`. No logic, props, data flow, or API changes —
purely presentational.
