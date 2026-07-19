package resource

import (
	"fmt"

	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// This file defines the growing capability library: the five plug-in points
// (decoders, transforms, derivers, sanitizers, converters) that extend the
// engine along its stable normalization spine. Each is a package-level registry
// populated by init() (built-in capabilities) and read once by the loader at
// startup — so lookups need no locking (registration is single-goroutine init;
// the built DeclarativeSource captures resolved capabilities, never the maps).

// Node is the format-neutral view of a decoded response tree. A Decoder turns
// raw bytes into a Node; the normalizer walks it via dotted paths. Kept tiny
// (Get/Text/Attr) so a new decoder is cheap to add.
type Node interface {
	// Get returns the child nodes reachable by a dotted path (e.g. "feed.entry"
	// or "author.name"), flattening repeats encountered along the way.
	Get(path string) []Node
	// Text returns the element's character data.
	Text() string
	// Attr returns the named attribute's value ("" when absent).
	Attr(name string) string
}

// Decoder parses raw response bytes into a Node tree.
type Decoder interface {
	Decode([]byte) (Node, error)
}

// Transform is a pure string→string mapping for a single field value.
type Transform func(string) string

// TransformFactory builds a Transform from its (optional) YAML argument. It
// validates the arg type, returning a keyed error the loader surfaces fail-fast.
type TransformFactory func(arg any) (Transform, error)

// Deriver computes a field value from the WHOLE item node plus the Paper built
// so far (unlike Transform, which sees only one string). Needed where a value
// depends on multiple sibling nodes — e.g. arXiv's pdfUrl (links + derived id).
type Deriver func(entry Node, p *models.Paper) (string, error)

// Sanitizer neutralizes untrusted free-text (the security seam for text fields).
type Sanitizer func(string) string

// Converter turns fetched content bytes into Markdown (e.g. html-to-markdown).
type Converter func([]byte) (string, error)

// The five capability registries. Package-level maps, populated at init.
var (
	decoders   = map[string]Decoder{}
	transforms = map[string]TransformFactory{}
	derivers   = map[string]Deriver{}
	sanitizers = map[string]Sanitizer{}
	converters = map[string]Converter{}
)

// RegisterDecoder registers a response decoder under a format name (e.g. "atom-xml").
func RegisterDecoder(format string, d Decoder) { decoders[format] = d }

// RegisterTransform registers a string transform factory under a name (e.g. "normalize").
func RegisterTransform(name string, f TransformFactory) { transforms[name] = f }

// RegisterDeriver registers a node-aware deriver under a name (e.g. "arxiv-id").
func RegisterDeriver(name string, d Deriver) { derivers[name] = d }

// RegisterSanitizer registers a free-text sanitizer under a name (e.g. "arxiv-terms").
func RegisterSanitizer(name string, s Sanitizer) { sanitizers[name] = s }

// RegisterConverter registers a content converter under a name (e.g. "html-to-markdown").
func RegisterConverter(name string, c Converter) { converters[name] = c }

// lookupDecoder resolves a decoder or returns a clear "unknown" error the loader
// surfaces fail-fast at startup.
func lookupDecoder(format string) (Decoder, error) {
	d, ok := decoders[format]
	if !ok {
		return nil, fmt.Errorf("unknown decoder %q", format)
	}
	return d, nil
}

// lookupTransform resolves a transform factory by name.
func lookupTransform(name string) (TransformFactory, error) {
	f, ok := transforms[name]
	if !ok {
		return nil, fmt.Errorf("unknown transform %q", name)
	}
	return f, nil
}

// lookupDeriver resolves a deriver by name.
func lookupDeriver(name string) (Deriver, error) {
	d, ok := derivers[name]
	if !ok {
		return nil, fmt.Errorf("unknown deriver %q", name)
	}
	return d, nil
}

// lookupSanitizer resolves a sanitizer by name.
func lookupSanitizer(name string) (Sanitizer, error) {
	s, ok := sanitizers[name]
	if !ok {
		return nil, fmt.Errorf("unknown sanitizer %q", name)
	}
	return s, nil
}

// lookupConverter resolves a content converter by name.
func lookupConverter(name string) (Converter, error) {
	c, ok := converters[name]
	if !ok {
		return nil, fmt.Errorf("unknown converter %q", name)
	}
	return c, nil
}
