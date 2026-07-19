package orchestrator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// This file owns POST /discover body parsing + the legacy back-compat shim,
// kept separate from the handler logic to keep orchestrator.go small.

// maxDiscoverBody caps the request body read (a discovery body is tiny).
const maxDiscoverBody = 64 * 1024

// defaultResourceID is the resource an empty/legacy body falls back to, so
// existing clients (and an empty body) keep working through the migration.
const defaultResourceID = "arxiv"

// discoverRequest is the current POST /discover body: which resource, and the
// validated field values for it.
type discoverRequest struct {
	ResourceID string            `json:"resourceId"`
	Values     map[string]string `json:"values"`
}

// legacyDiscoverBody is the pre-engine {category, terms} shape. Decoded STRICTLY
// as strings so a type mismatch is a 400, not a silent drop (F17).
type legacyDiscoverBody struct {
	Category string `json:"category"`
	Terms    string `json:"terms"`
}

// parseDiscover reads the request body and returns the target resource id and
// the raw (unvalidated) field values. It handles three shapes:
//   - empty/absent body  → default resource, empty values (back-compat)
//   - {resourceId, values} → used directly
//   - legacy {category, terms} (no values key) → folded into values (F17)
//
// The raw body is retained so the legacy fold sees top-level keys a typed decode
// into discoverRequest would silently drop. Validation/sanitization happens after
// this, via the resource's own schema.
func parseDiscover(r *http.Request) (resourceID string, values map[string]string, err error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxDiscoverBody))
	if err != nil {
		return "", nil, fmt.Errorf("invalid request body")
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return defaultResourceID, map[string]string{}, nil // empty body → defaults
	}

	var req discoverRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "", nil, fmt.Errorf("invalid request body")
	}
	resourceID = req.ResourceID
	if resourceID == "" {
		resourceID = defaultResourceID
	}

	if req.Values != nil {
		return resourceID, req.Values, nil
	}

	// No values key → fold legacy {category, terms}. Strict string decode so a
	// wrong-typed legacy field is a clear 400. Folded values get the same
	// whitelist + sanitizer as native values downstream.
	var legacy legacyDiscoverBody
	if err := json.Unmarshal(body, &legacy); err != nil {
		return "", nil, fmt.Errorf("invalid request body")
	}
	values = map[string]string{}
	if legacy.Category != "" {
		values["category"] = legacy.Category
	}
	if legacy.Terms != "" {
		values["terms"] = legacy.Terms
	}
	return resourceID, values, nil
}
