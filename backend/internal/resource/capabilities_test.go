package resource

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// --- capability registry lookups ---

func TestLookupUnknownCapabilities(t *testing.T) {
	if _, err := lookupDecoder("nope"); err == nil {
		t.Error("expected unknown decoder error")
	}
	if _, err := lookupTransform("nope"); err == nil {
		t.Error("expected unknown transform error")
	}
	if _, err := lookupDeriver("nope"); err == nil {
		t.Error("expected unknown deriver error")
	}
	if _, err := lookupSanitizer("nope"); err == nil {
		t.Error("expected unknown sanitizer error")
	}
	if _, err := lookupConverter("nope"); err == nil {
		t.Error("expected unknown converter error")
	}
}

func TestRegisterAndLookup(t *testing.T) {
	RegisterTransform("upper-test", func(any) (Transform, error) {
		return func(s string) string { return s }, nil
	})
	if _, err := lookupTransform("upper-test"); err != nil {
		t.Fatalf("registered transform not found: %v", err)
	}
}

// --- QueryPart custom unmarshalling (scalar + struct + rejection) ---

func TestQueryPartScalar(t *testing.T) {
	var q QueryPart
	if err := yaml.Unmarshal([]byte(`"submittedDate"`), &q); err != nil {
		t.Fatalf("unmarshal scalar: %v", err)
	}
	if !q.IsLiteral() || q.Literal != "submittedDate" {
		t.Fatalf("scalar QueryPart wrong: %+v", q)
	}
}

func TestQueryPartJoinParts(t *testing.T) {
	src := `
join: " AND "
parts:
  - value: "cat:{{category}}"
  - value: "all:{{terms}}"
    when: terms
`
	var q QueryPart
	if err := yaml.Unmarshal([]byte(src), &q); err != nil {
		t.Fatalf("unmarshal join/parts: %v", err)
	}
	if q.IsLiteral() {
		t.Fatalf("join/parts should not be literal")
	}
	if q.Join != " AND " || len(q.Parts) != 2 {
		t.Fatalf("join/parts wrong: %+v", q)
	}
	if q.Parts[1].When != "terms" {
		t.Fatalf("part when not parsed: %+v", q.Parts[1])
	}
}

func TestQueryPartRejectsSequence(t *testing.T) {
	var q QueryPart
	if err := yaml.Unmarshal([]byte(`[a, b]`), &q); err == nil {
		t.Fatalf("expected error unmarshalling a sequence into QueryPart")
	}
}

// --- TransformSpec custom unmarshalling ---

func TestTransformSpecScalar(t *testing.T) {
	var ts TransformSpec
	if err := yaml.Unmarshal([]byte(`normalize`), &ts); err != nil {
		t.Fatalf("unmarshal scalar transform: %v", err)
	}
	if ts.Name != "normalize" || ts.Arg != nil {
		t.Fatalf("scalar transform wrong: %+v", ts)
	}
}

func TestTransformSpecSingleKeyMap(t *testing.T) {
	var ts TransformSpec
	if err := yaml.Unmarshal([]byte(`{afterLast: "/"}`), &ts); err != nil {
		t.Fatalf("unmarshal map transform: %v", err)
	}
	if ts.Name != "afterLast" || ts.Arg != "/" {
		t.Fatalf("map transform wrong: %+v", ts)
	}
}

func TestTransformSpecRejectsMultiKeyMap(t *testing.T) {
	var ts TransformSpec
	if err := yaml.Unmarshal([]byte(`{a: 1, b: 2}`), &ts); err == nil {
		t.Fatalf("expected error: multi-key transform map is nondeterministic")
	}
}

// A FieldMap with a transforms list containing both forms must parse.
func TestFieldMapTransformsMixedForms(t *testing.T) {
	src := `
path: title
transforms:
  - normalize
  - afterLast: "/"
`
	var fm FieldMap
	if err := yaml.Unmarshal([]byte(src), &fm); err != nil {
		t.Fatalf("unmarshal fieldmap: %v", err)
	}
	if fm.Path != "title" || len(fm.Transforms) != 2 {
		t.Fatalf("fieldmap wrong: %+v", fm)
	}
	if fm.Transforms[0].Name != "normalize" || fm.Transforms[1].Name != "afterLast" {
		t.Fatalf("transform names wrong: %+v", fm.Transforms)
	}
}
