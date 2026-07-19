package resource

// Field type constants. v1 implements exactly what arXiv exercises: a
// whitelist-validated select and a sanitized free-text field. New types are
// added at the (Phase 06) UI + (Phase 05) validator seams only when a real
// resource needs one.
const (
	FieldSelect = "select"
	FieldText   = "text"
)

// Descriptor is the UI-facing description of a resource: its identity plus the
// request fields the frontend renders dynamically. Served by GET /resources;
// the JSON tags are the wire contract mirrored in frontend/lib/types.ts.
type Descriptor struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Description string  `json:"description"`
	Fields      []Field `json:"fields"`
}

// Field describes one request input the UI renders by Type. Options is present
// only for select fields; Default seeds the initial form value.
type Field struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Label    string   `json:"label"`
	Required bool     `json:"required"`
	Default  string   `json:"default,omitempty"`
	Options  []Option `json:"options,omitempty"`
}

// Option is one select choice: its submitted value plus a human label. It is
// both a catalog row (yaml) and a wire field (json), so it carries both tag sets.
type Option struct {
	Value string `json:"value" yaml:"value"`
	Label string `json:"label" yaml:"label"`
}
