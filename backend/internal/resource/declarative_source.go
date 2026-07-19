package resource

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// Content-stage sentinels. ErrContentNotFound is the recoverable re-pick signal
// (old ErrPaperHTMLNotFound); ErrContentFailed covers a fetch/convert failure
// (old ErrPaperHTMLFailed); ErrDecode is a response parse failure (old
// ErrArxivParse) — never embedding the response body.
var (
	ErrDecode          = errors.New("failed to decode response")
	ErrContentNotFound = errors.New("content not found (recoverable re-pick)")
	ErrContentFailed   = errors.New("failed to fetch or convert content")
)

// DeclarativeSource is the single Source implementation for v1: a resolved
// Declaration plus the wired engine stages (transport → decoder → normalizer →
// converter). All arXiv-ness lives in the Declaration; this type is generic.
type DeclarativeSource struct {
	decl       Declaration
	descriptor Descriptor
	decoder    Decoder
	response   compiledResponse
	converter  Converter
	sanitizers map[string]Sanitizer // by field name (text fields only)

	disco   *transport
	content *transport
	// content SSRF anchor: a resolved content URL's host/scheme must equal these.
	contentHost   string
	contentScheme string
}

// NewDeclarativeSource compiles a resolved Declaration into a runnable Source.
// The loader (Phase 03) has already resolved ${...} references and inlined
// catalog options; this constructor resolves capabilities (fail-fast on an
// unknown decoder/converter/transform/deriver/sanitizer) and builds the HTTP
// clients with the per-FetchSpec timeouts and the content redirect policy.
func NewDeclarativeSource(decl Declaration) (*DeclarativeSource, error) {
	dec, err := lookupDecoder(decl.Response.Format)
	if err != nil {
		return nil, err
	}
	conv, err := lookupConverter(decl.Content.Convert)
	if err != nil {
		return nil, err
	}
	cr, err := compileResponse(decl.Response, decl.ID)
	if err != nil {
		return nil, err
	}

	sans := map[string]Sanitizer{}
	for _, f := range decl.Request.Fields {
		if f.Sanitize == "" {
			continue
		}
		s, err := lookupSanitizer(f.Sanitize)
		if err != nil {
			return nil, err
		}
		sans[f.Name] = s
	}

	// The content URL's static prefix (before {{paper.id}}) fixes the SSRF anchor.
	base := decl.Content.Request.URL
	if i := strings.Index(base, "{{"); i != -1 {
		base = base[:i]
	}
	cu, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("invalid content url: %w", err)
	}

	ds := &DeclarativeSource{
		decl:          decl,
		descriptor:    buildDescriptor(decl),
		decoder:       dec,
		response:      cr,
		converter:     conv,
		sanitizers:    sans,
		contentHost:   cu.Host,
		contentScheme: cu.Scheme,
		disco: &transport{
			client:      &http.Client{Timeout: time.Duration(decl.Fetch.TimeoutSeconds) * time.Second},
			backoffUnit: time.Second,
		},
		content: &transport{
			client: &http.Client{
				Timeout:       time.Duration(decl.Content.Request.TimeoutSeconds) * time.Second,
				CheckRedirect: boundedSameHost(cu.Scheme, cu.Host, 3),
			},
			backoffUnit: time.Second,
		},
	}
	return ds, nil
}

// ID returns the resource identifier.
func (ds *DeclarativeSource) ID() string { return ds.decl.ID }

// Descriptor returns the UI-facing field schema.
func (ds *DeclarativeSource) Descriptor() Descriptor { return ds.descriptor }

// PageSize returns the declared per-page item count — the single owner of page
// size (F9), read by the orchestrator for both the cursor step and hasMore.
func (ds *DeclarativeSource) PageSize() int { return ds.decl.Fetch.Paginate.PageSize }

// Discover validates+sanitizes the request values against this source's own
// schema (F21 — safety is a property of the Source, so discover-more, the golden
// test, and rehydrated sessions all get it), builds the query, fetches, decodes,
// and normalizes into canonical Papers.
func (ds *DeclarativeSource) Discover(ctx context.Context, req Request, start int, onRetry func(attempt int)) ([]models.Paper, error) {
	values, err := ds.ValidateValues(req.Values)
	if err != nil {
		return nil, err
	}

	vars := runtimeVars{"start": strconv.Itoa(start)}
	maps.Copy(vars, values)

	q := buildQuery(ds.decl.Fetch.Query, vars)
	reqURL := ds.decl.Fetch.URL + "?" + q.Encode()

	body, _, err := ds.disco.fetch(ctx, fetchRequest{
		method:   ds.decl.Fetch.Method,
		url:      reqURL,
		headers:  ds.decl.Fetch.Headers,
		retry:    ds.decl.Fetch.Retry,
		maxBytes: ds.decl.Fetch.MaxBytes,
	}, onRetry)
	if err != nil {
		return nil, err
	}

	root, err := ds.decoder.Decode(body)
	if err != nil {
		return nil, ErrDecode // never echo the body (F14)
	}
	return ds.response.normalize(root), nil
}

// FetchContent fetches a single item's content and converts it to Markdown. A
// 404 with not_found:repick returns the recoverable ErrContentNotFound.
func (ds *DeclarativeSource) FetchContent(ctx context.Context, paperID string) (string, error) {
	reqURL, err := ds.buildContentURL(paperID)
	if err != nil {
		return "", err
	}
	body, status, err := ds.content.fetch(ctx, fetchRequest{
		method:   ds.decl.Content.Request.Method,
		url:      reqURL,
		headers:  ds.decl.Content.Request.Headers,
		retry:    ds.decl.Content.Request.Retry,
		maxBytes: ds.decl.Content.Request.MaxBytes,
	}, nil)
	// 404 is a recoverable re-pick, not a hard failure — check status before err.
	if status == http.StatusNotFound && ds.decl.Content.NotFound == "repick" {
		return "", ErrContentNotFound
	}
	if err != nil {
		return "", err
	}
	md, err := ds.converter(body)
	if err != nil {
		return "", fmt.Errorf("%w: conversion failed", ErrContentFailed)
	}
	return md, nil
}

// buildContentURL substitutes the (safety-checked) paper id into the content URL
// template and enforces the SSRF anchor: the resolved host+scheme must equal the
// declared base (V3 minimal). safePaperID blocks path traversal / scheme
// injection while still permitting old-style ids' embedded slash.
func (ds *DeclarativeSource) buildContentURL(paperID string) (string, error) {
	if !safePaperID(paperID) {
		return "", fmt.Errorf("%w: unsafe paper id", ErrContentFailed)
	}
	reqURL := strings.ReplaceAll(ds.decl.Content.Request.URL, "{{paper.id}}", paperID)
	u, err := url.Parse(reqURL)
	if err != nil {
		return "", fmt.Errorf("%w: bad content url", ErrContentFailed)
	}
	if u.Host != ds.contentHost || u.Scheme != ds.contentScheme {
		return "", fmt.Errorf("%w: content host mismatch", ErrContentFailed)
	}
	return reqURL, nil
}

// safePaperID is the generic path-injection guard: no traversal, no scheme, no
// leading slash, printable and constrained to alnum plus `. / -` (old-style
// arXiv ids embed a slash). The arXiv-id-specific format regex and the
// private-IP/metadata denylist are documented deferred hardening (V3, resource #2).
func safePaperID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	if strings.Contains(id, "..") || strings.Contains(id, "://") || strings.HasPrefix(id, "/") {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '/', r == '-':
		default:
			return false
		}
	}
	return true
}

// ValidateValues validates and sanitizes request values against the declaration:
// applies defaults for omitted fields, rejects a select value outside its catalog
// (generalizes IsValid), runs each text field's named sanitizer, and rejects a
// missing required field or an unknown field key. Returns the clean value map.
func (ds *DeclarativeSource) ValidateValues(in map[string]string) (map[string]string, error) {
	known := make(map[string]bool, len(ds.decl.Request.Fields))
	for _, f := range ds.decl.Request.Fields {
		known[f.Name] = true
	}
	for k := range in {
		if !known[k] {
			return nil, fmt.Errorf("unknown field %q", k)
		}
	}

	out := make(map[string]string, len(ds.decl.Request.Fields))
	for _, f := range ds.decl.Request.Fields {
		v := strings.TrimSpace(in[f.Name])
		if v == "" && f.Default != "" {
			v = f.Default // apply default for an omitted field
		}
		if v == "" {
			if f.Required {
				return nil, fmt.Errorf("missing required field %q", f.Name)
			}
			continue // omit empty optional
		}
		switch f.Type {
		case FieldSelect:
			if !ds.inCatalog(f, v) {
				return nil, fmt.Errorf("invalid value for %q", f.Name)
			}
		case FieldText:
			if s, ok := ds.sanitizers[f.Name]; ok {
				v = s(v)
			}
		}
		if v != "" {
			out[f.Name] = v
		}
	}
	return out, nil
}

// inCatalog reports whether v is one of the field's select options.
func (ds *DeclarativeSource) inCatalog(f FieldSpec, v string) bool {
	for _, opt := range f.Options.Values {
		if opt.Value == v {
			return true
		}
	}
	return false
}

// buildDescriptor projects a Declaration's request fields into the UI descriptor.
func buildDescriptor(decl Declaration) Descriptor {
	d := Descriptor{ID: decl.ID, Label: decl.Label, Description: decl.Description}
	for _, f := range decl.Request.Fields {
		fld := Field{Name: f.Name, Type: f.Type, Label: f.Label, Required: f.Required, Default: f.Default}
		if f.Type == FieldSelect {
			fld.Options = f.Options.Values
		}
		d.Fields = append(d.Fields, fld)
	}
	return d
}
