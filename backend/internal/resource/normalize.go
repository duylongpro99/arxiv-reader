package resource

import (
	"errors"
	"log/slog"

	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// This file IS the Normalization Layer (the Anti-Corruption Layer): the single
// place a decoded response tree becomes canonical models.Paper. Everything above
// it is byte/format handling; everything below it (orchestrator, agents) sees
// only canonical Papers. All field-mapping semantics live here.

// ErrNormalize signals the response could not be turned into any Papers. Per F10
// a single malformed item is skipped (not fatal); this is reserved for a
// structural failure.
var ErrNormalize = errors.New("failed to normalize response")

// paperFieldOrder fixes the assignment order so a deriver that reads an
// already-assigned field observes it: arxiv-pdf-url reads p.ID, so `id` must be
// resolved before `pdfUrl`. A stable order also makes normalization
// deterministic regardless of Go's random map iteration.
var paperFieldOrder = []string{"id", "title", "abstract", "authors", "published", "pdfUrl"}

// knownPaperFields is the set of assignable canonical field keys; the loader
// rejects any response field key outside it.
var knownPaperFields = map[string]bool{
	"id": true, "title": true, "abstract": true,
	"authors": true, "published": true, "pdfUrl": true,
}

// compiledField is a FieldMap with its capabilities resolved once at load: a
// deriver, OR a path (+ optional attr/multi) with a resolved transform chain.
type compiledField struct {
	path       string
	attr       string
	multi      bool
	transforms []Transform
	deriver    Deriver
}

// compiledResponse is the response spec with capabilities resolved and the item
// path + require list captured. Built once per source (fail-fast at load).
type compiledResponse struct {
	resourceID string
	items      string
	require    []string
	fields     map[string]compiledField
}

// compileResponse resolves every FieldMap's transforms/deriver against the
// registries, returning a keyed "unknown capability" error the loader surfaces.
func compileResponse(spec ResponseSpec, resourceID string) (compiledResponse, error) {
	cr := compiledResponse{
		resourceID: resourceID,
		items:      spec.Items,
		require:    spec.Require,
		fields:     make(map[string]compiledField, len(spec.Fields)),
	}
	for key, fm := range spec.Fields {
		cf := compiledField{path: fm.Path, attr: fm.Attr, multi: fm.Multi}
		if fm.Derive != "" {
			d, err := lookupDeriver(fm.Derive)
			if err != nil {
				return cr, err
			}
			cf.deriver = d
		} else {
			for _, ts := range fm.Transforms {
				factory, err := lookupTransform(ts.Name)
				if err != nil {
					return cr, err
				}
				t, err := factory(ts.Arg)
				if err != nil {
					return cr, err
				}
				cf.transforms = append(cf.transforms, t)
			}
		}
		cr.fields[key] = cf
	}
	return cr, nil
}

// normalize maps every item node under the item path to a canonical Paper. An
// item that errors in a deriver, or that fails the `require` check, is SKIPPED
// and logged — never fatal to the batch (F10, matching the old tolerant
// entryToPaper). A well-formed empty response yields an empty slice, no error.
func (cr compiledResponse) normalize(root Node) []models.Paper {
	items := root.Get(cr.items)
	papers := make([]models.Paper, 0, len(items))
	for _, item := range items {
		p, err := cr.resolveItem(item)
		if err != nil {
			slog.Warn("normalize: item skipped (deriver error)",
				"resource", cr.resourceID, "error", err.Error())
			continue
		}
		if !cr.requireOK(p) {
			slog.Warn("normalize: item missing required field(s), skipped",
				"resource", cr.resourceID, "require", cr.require)
			continue
		}
		papers = append(papers, p)
	}
	return papers
}

// resolveItem builds one Paper from an item node, assigning fields in the fixed
// order so derivers see prior assignments.
func (cr compiledResponse) resolveItem(item Node) (models.Paper, error) {
	p := models.Paper{Source: cr.resourceID}
	for _, key := range paperFieldOrder {
		cf, ok := cr.fields[key]
		if !ok {
			continue
		}
		if cf.deriver != nil {
			v, err := cf.deriver(item, &p)
			if err != nil {
				return p, err
			}
			assignScalar(&p, key, v)
			continue
		}
		if cf.multi {
			assignMulti(&p, key, resolveMulti(item, cf))
		} else {
			assignScalar(&p, key, resolveScalar(item, cf))
		}
	}
	return p, nil
}

// resolveScalar reads the first matching node's text (or attr), then applies the
// transform chain.
func resolveScalar(item Node, cf compiledField) string {
	nodes := item.Get(cf.path)
	if len(nodes) == 0 {
		return ""
	}
	v := nodes[0].Text()
	if cf.attr != "" {
		v = nodes[0].Attr(cf.attr)
	}
	for _, t := range cf.transforms {
		v = t(v)
	}
	return v
}

// resolveMulti reads every matching node, applies the transform chain to each,
// and DROPS empty results (F20 — e.g. an empty <author><name/>).
func resolveMulti(item Node, cf compiledField) []string {
	nodes := item.Get(cf.path)
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		v := n.Text()
		if cf.attr != "" {
			v = n.Attr(cf.attr)
		}
		for _, t := range cf.transforms {
			v = t(v)
		}
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// requireOK reports whether every required field on the Paper is present.
func (cr compiledResponse) requireOK(p models.Paper) bool {
	for _, key := range cr.require {
		if scalarField(p, key) == "" && len(multiField(p, key)) == 0 {
			return false
		}
	}
	return true
}

// assignScalar / assignMulti / scalarField / multiField map canonical field keys
// to the Paper struct — the engine's knowledge of its own output model (not
// arXiv-specific).
func assignScalar(p *models.Paper, key, value string) {
	switch key {
	case "id":
		p.ID = value
	case "title":
		p.Title = value
	case "abstract":
		p.Abstract = value
	case "published":
		p.Published = value
	case "pdfUrl":
		p.PDFURL = value
	}
}

func assignMulti(p *models.Paper, key string, values []string) {
	if key == "authors" {
		p.Authors = values
	}
}

func scalarField(p models.Paper, key string) string {
	switch key {
	case "id":
		return p.ID
	case "title":
		return p.Title
	case "abstract":
		return p.Abstract
	case "published":
		return p.Published
	case "pdfUrl":
		return p.PDFURL
	}
	return ""
}

func multiField(p models.Paper, key string) []string {
	if key == "authors" {
		return p.Authors
	}
	return nil
}
