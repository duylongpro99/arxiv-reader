# Phase 01 — Engine Contracts + Capability Registries

**Priority:** High · **Status:** pending · **Depends on:** —

## Overview

Create the new `internal/resource` package: the `Source` interface the
orchestrator will depend on, the declaration structs (parsed YAML shape), the
descriptor/field-schema types served to the UI, and the four **capability
registries** (decoders, transforms, sanitizers, content-converters). No fetching
behavior yet — pure contracts + registration seams so Phase 02 plugs into a
stable spine.

## Why this package

`config`, `orchestrator`, and the loader all need the `Source` interface and
descriptor types. Like the old `arxivquery` leaf, `internal/resource` imports
only `internal/models` (for `Paper`) — no config/orchestrator — to avoid cycles.

## Files to Create

- `backend/internal/resource/source.go`
  - `type Source interface { ID() string; Descriptor() Descriptor; Discover(ctx, req Request, start int, onRetry func(int)) ([]models.Paper, error); FetchContent(ctx, paperID string) (string, error) }`
  - `type Request struct { Values map[string]string }` — validated field values.
- `backend/internal/resource/descriptor.go`
  - `type Descriptor struct { ID, Label, Description string; Fields []Field }`
  - `type Field struct { Name, Type, Label string; Required bool; Default string; Options []Option }`
  - `type Option struct { Value, Label string }`
  - Field `Type` constants: `FieldSelect = "select"`, `FieldText = "text"` (v1 only).
- `backend/internal/resource/declaration.go` — parsed YAML structs (yaml tags):
  - `Declaration{ ID, Label, Description; Request RequestSpec; Fetch FetchSpec; Response ResponseSpec; Content ContentSpec }`
  - `RequestSpec{ Fields []FieldSpec }`; `FieldSpec{ Name, Type, Label string; Required bool; Default string; Options OptionsSpec; Sanitize string }`
  - `OptionsSpec{ Catalog string; Values []Option }` (v1 uses `Catalog`).
  - `FetchSpec{ Method, URL string; Headers map[string]string; Query map[string]QueryPart; Paginate PaginateSpec; Retry RetrySpec; TimeoutSeconds int }`
  - `QueryPart` — either a literal string or `{ Join string; Parts []PartSpec }`; `PartSpec{ Value string; When string }`. Use a custom `UnmarshalYAML` to accept both scalar and struct forms.
  - `ResponseSpec{ Format, Items string; Fields map[string]FieldMap; Require []string }`
  - `FieldMap{ Path string; Multi bool; Attr string; Transforms []TransformSpec; FirstOf []FieldMap; Template string; Where map[string]string }`
  - `TransformSpec` — scalar name (`normalize`) or single-key map (`{afterLast: "/"}`); custom `UnmarshalYAML`.
  - `ContentSpec{ Request FetchSpec; Convert string; NotFound string }`
  - `RetrySpec{ MaxRetries int; On []string; BackoffBaseSeconds int; BackoffFactor int }`, `PaginateSpec{ Kind, Param string }`
- `backend/internal/resource/registry.go`
  - `type Registry struct { ... }` wrapping `map[string]Source`; `Register(Source)`, `Get(id) (Source, bool)`, `List() []Source` (stable-sorted by ID), `Descriptors() []Descriptor`.
- `backend/internal/resource/capabilities.go` — four registries + registration API:
  - `type Decoder interface { Decode([]byte) (Node, error) }` + `RegisterDecoder(format string, Decoder)` / `decoder(format) (Decoder, bool)`.
  - `type Node interface { ... }` — format-neutral tree: `Get(key string) []Node`, `Text() string`, `Attr(name string) string`. (Impl in Phase 02.)
  - `type Transform func(string) string` + `RegisterTransform(name, factory func(arg any) (Transform, error))`.
  - `type Sanitizer func(string) string` + `RegisterSanitizer(name, Sanitizer)`.
  - `type Converter func([]byte) (string, error)` + `RegisterConverter(name, Converter)` (content-converters, e.g. html-to-markdown).
  - All registries are package-level maps guarded by clear "unknown capability" errors used by the loader.
- Tests: `registry_test.go`, `capabilities_test.go` — register/get/list, unknown-capability errors, `QueryPart`/`TransformSpec` YAML unmarshalling (scalar + struct forms).

## Implementation Steps

1. Define `Source`, `Request`, `Descriptor`, `Field`, `Option`.
2. Define declaration structs with yaml tags + the two custom `UnmarshalYAML`
   shims (QueryPart, TransformSpec) so the YAML in Phase 03 parses cleanly.
3. Define `Registry` (source registry) and the four capability registries with
   register/lookup + typed "unknown X %q" errors.
4. Declare `Node` and capability interfaces (no impl — Phase 02).
5. Tests for registry behavior + the custom unmarshallers.

## Red Team Fixes (2026-07-18) — applied

- **F5 (H1):** `RetrySpec` gains `TransientStatuses []int` + `TimeoutTerminal bool` so
  transient/timeout classification is per-`FetchSpec` (discovery: timeout transient;
  content: timeout terminal). Not one global policy.
- **F15 (H11):** v1 `FieldMap` = `Path` / `Attr` / `Multi` / `Transforms` ONLY —
  **drop `FirstOf` / `Where` / `Template`** (arXiv id + pdfURL move to Go transforms,
  see Phase 02 F2/F3). Keep `QueryPart{Join,Parts,When}`. Spec the remaining
  mini-DSL's error semantics; budget it as new code with a test matrix.
- **F19 (M4):** `QueryPart` + `TransformSpec` `UnmarshalYAML` MUST reject multi-key
  maps and wrong-typed args with a keyed error (Go map iteration is random →
  nondeterministic otherwise). Test the rejection paths, not just happy forms.

<!-- Updated: Validation Session 1 — V1 deriver capability -->
**V1 — add a `Deriver` capability** alongside the four registries: `type Deriver
func(entry Node, p *models.Paper) (string, error)` + `RegisterDeriver(name, Deriver)`.
Node-aware (sees the whole entry + partial Paper), unlike `Transform func(string)string`.
`FieldMap` gains a `Derive string` field (mutually exclusive with `Path`/`Transforms`).

## Todo

- [ ] `source.go`, `descriptor.go`, `declaration.go`
- [ ] `registry.go`, `capabilities.go`
- [ ] custom `UnmarshalYAML` for `QueryPart` + `TransformSpec`
- [ ] unit tests
- [ ] `go build ./... && go test ./internal/resource/...`

## Success Criteria

- Package compiles importing only `internal/models`.
- Registries register/lookup with clear errors; dual-form YAML nodes unmarshal.
- No behavior yet — Phase 02 fills the interfaces.

## Risks

- Over-modeling the declaration structs → keep to fields arXiv actually uses;
  add struct fields only when a capability needs them (YAGNI).
- `Node` interface shape churn once Phase 02 implements the XML decoder — accept
  minor iteration; keep the interface tiny (Get/Text/Attr).
