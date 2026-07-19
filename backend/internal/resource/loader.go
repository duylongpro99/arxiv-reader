package resource

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// maxYAMLBytes caps a declaration/catalog file size (F16 — a hostile or runaway
// file must not exhaust memory at load).
const maxYAMLBytes = 1 << 20 // 1 MiB

// catalogsSubdir is the reserved subdirectory holding option catalogs; it is NOT
// scanned for declarations.
const catalogsSubdir = "catalogs"

// refRe matches a ${VAR} config reference (screaming-snake keys only).
var refRe = regexp.MustCompile(`\$\{([A-Z0-9_]+)\}`)

// catalogNameRe constrains a catalog name to a safe slug (F16 — no path traversal).
var catalogNameRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// plainScalarRe is the conservative set of characters safe to splice into YAML as
// a bare plain scalar (numbers, urls, slugs). Anything else is double-quoted.
var plainScalarRe = regexp.MustCompile(`^[A-Za-z0-9._:/@+-]+$`)

// Load reads every resources/*.yaml (skipping catalogs/), resolves ${...} config
// references on the RAW bytes before parsing (so numeric slots parse as ints —
// F11), loads referenced option catalogs, validates fail-fast, builds a
// DeclarativeSource, and registers it. An empty result is an error — the server
// must have at least one resource.
func Load(dir string, resolve func(key string) (string, error)) (*Registry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read resources dir: %w", err)
	}
	reg := NewRegistry()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		decl, err := loadDeclaration(path, dir, resolve)
		if err != nil {
			return nil, fmt.Errorf("resource %s: %w", e.Name(), err)
		}
		src, err := NewDeclarativeSource(decl)
		if err != nil {
			return nil, fmt.Errorf("resource %s: %w", e.Name(), err)
		}
		if err := reg.Register(src); err != nil {
			return nil, err
		}
	}
	if reg.Len() == 0 {
		return nil, fmt.Errorf("no resources found in %s (need at least one *.yaml)", dir)
	}
	return reg, nil
}

// loadDeclaration reads + resolves + parses + catalog-loads + validates one file.
func loadDeclaration(path, dir string, resolve func(string) (string, error)) (Declaration, error) {
	var decl Declaration
	raw, err := readCapped(path)
	if err != nil {
		return decl, err
	}
	resolved, err := resolveRefs(raw, resolve)
	if err != nil {
		return decl, err
	}
	if err := yaml.Unmarshal(resolved, &decl); err != nil {
		return decl, fmt.Errorf("parse error: %w", err)
	}
	if err := loadFieldCatalogs(&decl, dir); err != nil {
		return decl, err
	}
	if err := validate(decl); err != nil {
		return decl, err
	}
	return decl, nil
}

// resolveRefs replaces every ${VAR} with its resolved value, YAML-quoting/escaping
// values that are not safe bare scalars and rejecting any resolved value with
// control characters (V2 — prevents structure injection). Only value-position
// references are used by v1 declarations.
func resolveRefs(raw []byte, resolve func(string) (string, error)) ([]byte, error) {
	var firstErr error
	out := refRe.ReplaceAllFunc(raw, func(m []byte) []byte {
		key := string(refRe.FindSubmatch(m)[1])
		val, err := resolve(key)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return m
		}
		if hasControlChars(val) {
			if firstErr == nil {
				firstErr = fmt.Errorf("resolved value for %q contains control characters", key)
			}
			return m
		}
		return []byte(yamlScalar(val))
	})
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// yamlScalar renders a resolved value as a YAML scalar: bare when safe (numbers,
// urls — so int fields still parse), double-quoted+escaped otherwise.
func yamlScalar(v string) string {
	if v != "" && plainScalarRe.MatchString(v) {
		return v
	}
	esc := strings.ReplaceAll(v, `\`, `\\`)
	esc = strings.ReplaceAll(esc, `"`, `\"`)
	return `"` + esc + `"`
}

// hasControlChars reports whether s contains any ASCII control char (incl. CR/LF/NUL).
func hasControlChars(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// loadFieldCatalogs resolves each select field's referenced catalog into inline
// options. The catalog name is slug-constrained and the resolved path is
// containment-checked under dir/catalogs (F16).
func loadFieldCatalogs(decl *Declaration, dir string) error {
	catalogsDir := filepath.Join(dir, catalogsSubdir)
	for i := range decl.Request.Fields {
		f := &decl.Request.Fields[i]
		name := f.Options.Catalog
		if name == "" {
			continue
		}
		if !catalogNameRe.MatchString(name) {
			return fmt.Errorf("field %q: invalid catalog name %q", f.Name, name)
		}
		p := filepath.Join(catalogsDir, name+".yaml")
		if err := ensureContained(catalogsDir, p); err != nil {
			return err
		}
		opts, err := loadCatalog(p)
		if err != nil {
			return fmt.Errorf("field %q: %w", f.Name, err)
		}
		f.Options.Values = opts
	}
	return nil
}

// ensureContained rejects a resolved path that escapes its base directory.
func ensureContained(base, p string) error {
	absBase, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	absP, err := filepath.Abs(p)
	if err != nil {
		return err
	}
	if absP != absBase && !strings.HasPrefix(absP, absBase+string(os.PathSeparator)) {
		return fmt.Errorf("catalog path escapes catalogs dir")
	}
	return nil
}

// readCapped reads a file, failing if it exceeds maxYAMLBytes.
func readCapped(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxYAMLBytes {
		return nil, fmt.Errorf("file %s exceeds %d bytes", filepath.Base(path), maxYAMLBytes)
	}
	return os.ReadFile(path)
}

// validate enforces declaration integrity fail-fast with clear, key-free
// messages: identity present; field types valid; each select has a resolvable
// catalog (>=1 option) and an in-catalog default; every referenced capability
// (decoder/transform/deriver/sanitizer/converter) exists; response field keys are
// canonical; require fields are mapped; declared URLs pass the egress policy.
func validate(d Declaration) error {
	if d.ID == "" {
		return fmt.Errorf("id is required")
	}
	if d.Label == "" {
		return fmt.Errorf("label is required")
	}

	for _, f := range d.Request.Fields {
		switch f.Type {
		case FieldSelect:
			if len(f.Options.Values) == 0 {
				return fmt.Errorf("field %q: select has no options (catalog empty or missing)", f.Name)
			}
			if f.Default != "" && !optionExists(f.Options.Values, f.Default) {
				return fmt.Errorf("field %q: default %q is not in its catalog", f.Name, f.Default)
			}
		case FieldText:
			if f.Sanitize != "" {
				if _, err := lookupSanitizer(f.Sanitize); err != nil {
					return fmt.Errorf("field %q: %w", f.Name, err)
				}
			}
		default:
			return fmt.Errorf("field %q: unknown type %q (want select|text)", f.Name, f.Type)
		}
	}

	if _, err := lookupDecoder(d.Response.Format); err != nil {
		return err
	}
	for key, fm := range d.Response.Fields {
		if !knownPaperFields[key] {
			return fmt.Errorf("response field %q is not a known paper field", key)
		}
		if fm.Derive != "" {
			if _, err := lookupDeriver(fm.Derive); err != nil {
				return err
			}
			continue
		}
		for _, ts := range fm.Transforms {
			if _, err := lookupTransform(ts.Name); err != nil {
				return err
			}
		}
	}
	for _, req := range d.Response.Require {
		if _, ok := d.Response.Fields[req]; !ok {
			return fmt.Errorf("require field %q is not mapped in response.fields", req)
		}
	}

	if _, err := lookupConverter(d.Content.Convert); err != nil {
		return err
	}

	if err := checkEgress(d.Fetch.URL); err != nil {
		return fmt.Errorf("fetch.url: %w", err)
	}
	if err := checkEgress(stripPlaceholders(d.Content.Request.URL)); err != nil {
		return fmt.Errorf("content.request.url: %w", err)
	}
	return nil
}

func optionExists(opts []Option, v string) bool {
	for _, o := range opts {
		if o.Value == v {
			return true
		}
	}
	return false
}
