package resource

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Declaration is the parsed shape of a resources/*.yaml file — the complete,
// declarative definition of one resource. The loader (Phase 03) resolves
// ${...} config references on the raw bytes, validates it, and builds a
// DeclarativeSource from it.
type Declaration struct {
	ID          string       `yaml:"id"`
	Label       string       `yaml:"label"`
	Description string       `yaml:"description"`
	Request     RequestSpec  `yaml:"request"`
	Fetch       FetchSpec    `yaml:"fetch"`
	Response    ResponseSpec `yaml:"response"`
	Content     ContentSpec  `yaml:"content"`
}

// RequestSpec is the declared input schema — the fields the UI renders and the
// engine validates before a fetch.
type RequestSpec struct {
	Fields []FieldSpec `yaml:"fields"`
}

// FieldSpec is one declared request field. Sanitize names a registered sanitizer
// applied to text values (the security seam); Options supplies select choices.
type FieldSpec struct {
	Name     string      `yaml:"name"`
	Type     string      `yaml:"type"`
	Label    string      `yaml:"label"`
	Required bool        `yaml:"required"`
	Default  string      `yaml:"default"`
	Options  OptionsSpec `yaml:"options"`
	Sanitize string      `yaml:"sanitize"`
}

// OptionsSpec sources select choices either from a named catalog file (v1) or
// inline values.
type OptionsSpec struct {
	Catalog string   `yaml:"catalog"`
	Values  []Option `yaml:"values"`
}

// FetchSpec is a single HTTP fetch: method/url/headers, a structured query, the
// pagination scheme, retry policy, and per-request timeout. The dual query form
// (literal vs join-of-parts) is what QueryPart's custom unmarshaller handles.
type FetchSpec struct {
	Method         string               `yaml:"method"`
	URL            string               `yaml:"url"`
	Headers        map[string]string    `yaml:"headers"`
	Query          map[string]QueryPart `yaml:"query"`
	Paginate       PaginateSpec         `yaml:"paginate"`
	Retry          RetrySpec            `yaml:"retry"`
	TimeoutSeconds int                  `yaml:"timeout_seconds"`
	// MaxBytes caps the response body via io.LimitReader (OOM guard, F14). Applied
	// to both the discovery and content fetches.
	MaxBytes int64 `yaml:"max_bytes"`
}

// PartSpec is one conditional query fragment: Value is spliced in only when the
// When runtime variable is non-empty (an empty When means always included).
type PartSpec struct {
	Value string `yaml:"value"`
	When  string `yaml:"when"`
}

// QueryPart is one query-parameter value: either a literal scalar, or a
// join-of-parts (parts assembled with Join, each part dropped when its When var
// is empty). The dual form is why it needs a custom unmarshaller.
type QueryPart struct {
	Literal   string
	Join      string
	Parts     []PartSpec
	isLiteral bool
}

// UnmarshalYAML accepts BOTH a scalar (literal value) and a mapping
// ({join, parts}). Any other node kind is a keyed error — an ambiguous form
// must fail loudly at load rather than parse into a surprising shape (F19).
func (q *QueryPart) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		q.Literal = value.Value
		q.isLiteral = true
		return nil
	case yaml.MappingNode:
		var m struct {
			Join  string     `yaml:"join"`
			Parts []PartSpec `yaml:"parts"`
		}
		if err := value.Decode(&m); err != nil {
			return err
		}
		q.Join, q.Parts, q.isLiteral = m.Join, m.Parts, false
		return nil
	default:
		return fmt.Errorf("query part must be a scalar or a {join, parts} mapping")
	}
}

// IsLiteral reports whether this part is a plain scalar value (vs a join-of-parts).
func (q QueryPart) IsLiteral() bool { return q.isLiteral }

// ResponseSpec describes how a decoded response tree becomes canonical Papers:
// the decoder Format, the Items path (the repeated element), the per-field
// mapping, and the Require list (fields that MUST be present per item).
type ResponseSpec struct {
	Format  string              `yaml:"format"`
	Items   string              `yaml:"items"`
	Fields  map[string]FieldMap `yaml:"fields"`
	Require []string            `yaml:"require"`
}

// FieldMap maps one Paper field from an item node. v1 supports either a Path
// (with optional @Attr, Multi, and a Transforms chain) OR a Derive (a
// node-aware deriver, e.g. arxiv-id / arxiv-pdf-url). Derive is mutually
// exclusive with Path/Transforms (F15 dropped firstOf/where/template; V1 added
// derive).
type FieldMap struct {
	Path       string          `yaml:"path"`
	Attr       string          `yaml:"attr"`
	Multi      bool            `yaml:"multi"`
	Transforms []TransformSpec `yaml:"transforms"`
	Derive     string          `yaml:"derive"`
}

// TransformSpec names a registered transform, optionally with an argument. It
// unmarshals from EITHER a scalar (bare name, e.g. `normalize`) or a single-key
// mapping (name → arg, e.g. `{afterLast: "/"}`).
type TransformSpec struct {
	Name string
	Arg  any
}

// UnmarshalYAML accepts a scalar name or a single-key mapping. A multi-key map
// is rejected: Go map iteration is random, so >1 key would make the applied
// transform nondeterministic (F19).
func (t *TransformSpec) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		t.Name = value.Value
		return nil
	case yaml.MappingNode:
		// Content is [key, val, key, val, ...]; exactly one pair is required.
		if len(value.Content) != 2 {
			return fmt.Errorf("transform must be a single-key mapping, got %d keys", len(value.Content)/2)
		}
		t.Name = value.Content[0].Value
		var arg any
		if err := value.Content[1].Decode(&arg); err != nil {
			return err
		}
		t.Arg = arg
		return nil
	default:
		return fmt.Errorf("transform must be a scalar name or a single-key mapping")
	}
}

// ContentSpec is the second fetch: pull one item's content and convert it.
// NotFound names the recovery behaviour on 404 ("repick").
type ContentSpec struct {
	Request  FetchSpec `yaml:"request"`
	Convert  string    `yaml:"convert"`
	NotFound string    `yaml:"not_found"`
}

// RetrySpec is the transient-failure retry policy for a FetchSpec. On lists the
// conditions treated as transient (retryable): "429", "5xx", "network",
// "timeout". Making the set per-FetchSpec is how divergent timeout semantics are
// expressed (F5): discovery lists "timeout" (retried); content omits it
// (terminal). This single mechanism subsumes the plan's separate
// TransientStatuses/TimeoutTerminal fields.
type RetrySpec struct {
	MaxRetries         int      `yaml:"max_retries"`
	On                 []string `yaml:"on"`
	BackoffBaseSeconds int      `yaml:"backoff_base_seconds"`
	BackoffFactor      int      `yaml:"backoff_factor"`
}

// PaginateSpec describes the pagination scheme: Kind (e.g. "offset") and the
// query Param that carries the cursor (e.g. "start").
type PaginateSpec struct {
	Kind  string `yaml:"kind"`
	Param string `yaml:"param"`
	// PageSize is the per-page item count — the single owner of page size (F9),
	// exposed via Source.PageSize() so the orchestrator stops duplicating it.
	PageSize int `yaml:"page_size"`
}
