## Security & Configuration Tips

- **Never** commit `.env`, API keys, session databases, Chroma data, logs, or exported reports. Keep safe defaults in `.env.example` only. Do not log credentials or raw HTML; persisted state should contain extracted text and metadata only, as described in the architecture.

## Core Principles

- **Simplicity First**: Make every change as simple as possible. Impact minimal code.
- **No Laziness**: Find root causes. No temporary fixes. Senior developer standards.
- **Minimat Impact**: Changes should only touch what's necessary. Avoid introducing bugs.

## Self-Improvement Loop

- After ANY correction from the user: update `docs/lessons/[lesstion-title].md` with the pattern
- Write rules for yourself that prevent the same mistake
- Ruthlessly iterate on these lessons until mistake rate drops
- Review lessons at session start for relevant project

## Demand Elegance (Balanced)

- For non-trivial changes: pause and ask "is there a more elegant way?"
- If a fix feels hacky: "Knowing everything I know now, implement the elegant solution"
- Skip this for simple, obvious fixes - don't over-engineer
- Challenge your own work before presenting it
-

## Task/Plan Management

- **Capture Lessons**: Update `docs/lessons/[lesstion-title].md` after corrections
- **Clean worktree**: If task/plan is executed in a worktree. Make sure **remove local worktree after finish task and push to origin**.
- **Document Up-to-date**: Before using any document, verify it matches the current codebase. If it references missing files, broken symbols, or outdated behavior — flag it, reconcile against live code, and update the stale sections before proceeding. Never propose a milestone based on assumptions you haven't confirmed in code.

## Engineering Principles

- Prefer designs that maximize clarity, adaptability, and change isolation while minimizing complexity and coupling.
- Preserve clear boundaries and maintain low coupling with high cohesion.
- Introduce abstractions only when they provide meaningful long-term value.

## Think Before Build

- **Understand the problem**: Identify the core responsibility of this feature, its boundaries, what it owns vs. delegates, and what is likely to change vs. stay stable.
- **Reason about structure**: Determine how data, logic, and side effects should be separated. Identify what should be abstracted to vary independently, and whether creation, lifecycle, state, or event behavior needs explicit modeling. Spot where tight coupling is a risk.
- **Check your complexity**: Verify every abstraction solves a real problem in this codebase. Prefer the simpler approach unless real complexity justifies otherwise.
- **Write a design note before any code**:
    - Template:
        ```
        Problem: <structural challenge this feature presents>
        Structure: <how responsibilities are divided and why>
        Tradeoffs: <what was considered and rejected>
        ```
    - Path: docs/design-notes/[filename].md
- **The right design is the simplest one that handles real complexity without collapsing under future change**.
