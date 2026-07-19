package resource

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// realResourcesDir is the shipped declaration dir, relative to this package.
const realResourcesDir = "../../../resources"

// prodResolve mimics config.Resolve for the shipped arxiv.yaml with test values.
func prodResolve(base, htmlBase string) func(string) (string, error) {
	m := map[string]string{
		"AGENT_ARXIV_CATEGORY":               "cs.AI",
		"AGENT_ARXIV_BASE_URL":               base,
		"AGENT_FETCH_LIMIT":                  "20",
		"AGENT_USER_AGENT":                   "arxiv-explainer-agent/test",
		"AGENT_REQUEST_TIMEOUT_SECONDS":      "5",
		"AGENT_MIN_REQUEST_INTERVAL_SECONDS": "1",
		"AGENT_MAX_RETRIES":                  "3",
		"AGENT_ARXIV_HTML_BASE_URL":          htmlBase,
		"AGENT_MAX_CONTENT_BYTES":            "52428800",
	}
	return func(key string) (string, error) {
		if v, ok := m[key]; ok {
			return v, nil
		}
		return "", fmt.Errorf("unknown key %q", key)
	}
}

func TestLoadRealArxiv(t *testing.T) {
	reg, err := Load(realResourcesDir, prodResolve("https://export.arxiv.org/api/query", "https://arxiv.org/html"))
	if err != nil {
		t.Fatalf("load real arxiv.yaml: %v", err)
	}
	src, ok := reg.Get("arxiv")
	if !ok {
		t.Fatal("arxiv not registered")
	}
	d := src.Descriptor()
	if d.ID != "arxiv" || len(d.Fields) != 2 {
		t.Fatalf("descriptor: %+v", d)
	}
	if d.Fields[0].Name != "category" || d.Fields[0].Type != FieldSelect || len(d.Fields[0].Options) != 40 {
		t.Fatalf("category field: %+v", d.Fields[0])
	}
	if d.Fields[1].Name != "terms" || d.Fields[1].Type != FieldText {
		t.Fatalf("terms field: %+v", d.Fields[1])
	}
	if src.PageSize() != 20 {
		t.Fatalf("PageSize = %d, want 20", src.PageSize())
	}
}

// --- temp-dir failure cases ---

const minimalDecl = `
id: test
label: Test
request:
  fields:
    - name: category
      type: select
      label: Category
      required: true
      default: ${CAT}
      options:
        catalog: test-cat
fetch:
  method: GET
  url: ${BASE}
  query:
    search_query: "cat:{{category}}"
  paginate: { kind: offset, param: start, page_size: 10 }
  retry: { max_retries: 0, on: ["429"], backoff_base_seconds: 1, backoff_factor: 2 }
  timeout_seconds: 5
  max_bytes: 1000
response:
  format: atom-xml
  items: feed.entry
  fields:
    id: { derive: arxiv-id }
    title: { path: title, transforms: [normalize] }
  require: [id, title]
content:
  request:
    method: GET
    url: ${BASE}/{{paper.id}}
    retry: { max_retries: 0, on: ["429"], backoff_base_seconds: 1, backoff_factor: 2 }
    timeout_seconds: 5
    max_bytes: 1000
  convert: html-to-markdown
  not_found: repick
`

func writeTempResources(t *testing.T, decl, catalog string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(decl), 0o600); err != nil {
		t.Fatal(err)
	}
	if catalog != "" {
		if err := os.MkdirAll(filepath.Join(dir, "catalogs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "catalogs", "test-cat.yaml"), []byte(catalog), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func tempResolve(key string) (string, error) {
	switch key {
	case "CAT":
		return "cs.AI", nil
	case "BASE":
		return "http://example.com", nil
	}
	return "", fmt.Errorf("unknown key %q", key)
}

func TestLoadValidTempDir(t *testing.T) {
	dir := writeTempResources(t, minimalDecl, "- { value: cs.AI, label: AI }\n")
	if _, err := Load(dir, tempResolve); err != nil {
		t.Fatalf("valid temp dir should load: %v", err)
	}
}

func TestLoadUnknownVarFails(t *testing.T) {
	decl := minimalDecl + "\n# ref ${MISSING_VAR}\n"
	dir := writeTempResources(t, decl, "- { value: cs.AI, label: AI }\n")
	if _, err := Load(dir, tempResolve); err == nil {
		t.Fatal("expected unknown ${VAR} to fail load")
	}
}

func TestLoadUnknownCapabilityFails(t *testing.T) {
	bad := strings.Replace(minimalDecl, "format: atom-xml", "format: bogus-xml", 1)
	dir := writeTempResources(t, bad, "- { value: cs.AI, label: AI }\n")
	if _, err := Load(dir, tempResolve); err == nil {
		t.Fatal("expected unknown decoder to fail load")
	}
}

func TestLoadMissingCatalogFails(t *testing.T) {
	dir := writeTempResources(t, minimalDecl, "") // no catalog file
	if _, err := Load(dir, tempResolve); err == nil {
		t.Fatal("expected missing catalog to fail load")
	}
}

func TestLoadDefaultNotInCatalogFails(t *testing.T) {
	dir := writeTempResources(t, minimalDecl, "- { value: cs.LG, label: ML }\n") // default cs.AI absent
	if _, err := Load(dir, tempResolve); err == nil {
		t.Fatal("expected default-not-in-catalog to fail load")
	}
}

// F11: a config.yaml-only value (no matching env var) still resolves and boots.
func TestLoadConfigOnlyValueBoots(t *testing.T) {
	dir := writeTempResources(t, minimalDecl, "- { value: cs.AI, label: AI }\n")
	// tempResolve returns CAT from an in-memory map, not os.Getenv — exactly the
	// F11 scenario (a category set in config.yaml with no .env override).
	if _, err := Load(dir, tempResolve); err != nil {
		t.Fatalf("config-only value should boot: %v", err)
	}
}

// V2: a resolved value containing a colon or a quote must not break YAML parsing
// or inject structure.
func TestResolveRefsInjectionSafe(t *testing.T) {
	resolve := func(key string) (string, error) {
		if key == "EVIL" {
			return `a: b "quoted" value`, nil
		}
		return "", fmt.Errorf("unknown")
	}
	out, err := resolveRefs([]byte("label: ${EVIL}\n"), resolve)
	if err != nil {
		t.Fatalf("resolveRefs: %v", err)
	}
	var m map[string]string
	if err := yaml.Unmarshal(out, &m); err != nil {
		t.Fatalf("resolved YAML did not parse (injection?): %v\n%s", err, out)
	}
	if m["label"] != `a: b "quoted" value` {
		t.Fatalf("value corrupted: %q", m["label"])
	}
}

// V2: a resolved value with control characters is rejected.
func TestResolveRefsRejectsControlChars(t *testing.T) {
	resolve := func(string) (string, error) { return "line1\nline2", nil }
	if _, err := resolveRefs([]byte("x: ${V}\n"), resolve); err == nil {
		t.Fatal("expected control-char value to be rejected")
	}
}
