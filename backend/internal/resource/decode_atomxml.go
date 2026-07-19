package resource

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
)

// atom-xml is the v1 decoder. It turns an Atom/XML document into a generic,
// namespace-agnostic Node tree matched by LOCAL element name — mirroring the old
// tool's `xml:"..."` local-name matching, so `feed.entry`, `author.name`, and
// link attributes resolve exactly as before. Kept generic (no arXiv shapes) so
// the arXiv-ness stays entirely in the YAML.

func init() { RegisterDecoder("atom-xml", atomXMLDecoder{}) }

type atomXMLDecoder struct{}

func (atomXMLDecoder) Decode(data []byte) (Node, error) { return decodeAtomXML(data) }

// xmlNode is one element in the decoded tree: its local name, attributes (by
// local name), accumulated character data, and child elements. It implements Node.
type xmlNode struct {
	name     string
	attrs    map[string]string
	text     string
	children []*xmlNode
}

// decodeAtomXML streams the document into a tree under a synthetic root whose
// children are the top-level elements — so Get("feed.entry") walks root→feed→entry.
func decodeAtomXML(data []byte) (Node, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	root := &xmlNode{}
	stack := []*xmlNode{root}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			node := &xmlNode{name: t.Name.Local, attrs: make(map[string]string, len(t.Attr))}
			for _, a := range t.Attr {
				node.attrs[a.Name.Local] = a.Value // last-wins on duplicate local names
			}
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, node)
			stack = append(stack, node)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			// Accumulate direct chardata; entities are already decoded by the parser.
			cur := stack[len(stack)-1]
			cur.text += string(t)
		}
	}
	return root, nil
}

// Get walks a dotted path from this node, matching children by local name at each
// segment and flattening repeats. E.g. from an entry, Get("author.name") returns
// every <name> under every <author>.
func (n *xmlNode) Get(path string) []Node {
	cur := []*xmlNode{n}
	for _, seg := range strings.Split(path, ".") {
		var next []*xmlNode
		for _, c := range cur {
			for _, ch := range c.children {
				if ch.name == seg {
					next = append(next, ch)
				}
			}
		}
		cur = next
	}
	out := make([]Node, len(cur))
	for i, c := range cur {
		out[i] = c
	}
	return out
}

// Text returns the element's raw accumulated character data (callers normalize/trim).
func (n *xmlNode) Text() string { return n.text }

// Attr returns the named attribute (by local name), or "" if absent.
func (n *xmlNode) Attr(name string) string { return n.attrs[name] }
