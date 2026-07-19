package resource

import (
	"strings"
	"testing"
)

// --- decoder: path / attr / multi lookups ---

func TestDecodeAtomXMLLookups(t *testing.T) {
	root, err := decodeAtomXML([]byte(sampleFeed))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	entries := root.Get("feed.entry")
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	// dotted path through repeats
	names := entries[0].Get("author.name")
	if len(names) != 2 || strings.TrimSpace(names[0].Text()) != "Ada Lovelace" {
		t.Fatalf("author.name lookup: %#v", names)
	}
	// attribute lookup on links
	links := entries[0].Get("link")
	if len(links) != 2 || links[1].Attr("type") != "application/pdf" {
		t.Fatalf("link attr lookup: %#v", links)
	}
}

// --- interpolate: parts / join / when + encoding ---

func TestBuildQueryJoinWithTerms(t *testing.T) {
	spec := map[string]QueryPart{
		"search_query": {Join: " AND ", Parts: []PartSpec{
			{Value: "cat:{{category}}"},
			{Value: "all:{{terms}}", When: "terms"},
		}},
		"start": {Literal: "{{start}}", isLiteral: true},
	}
	q := buildQuery(spec, runtimeVars{"category": "cs.AI", "terms": "transformer", "start": "0"})
	if got := q.Get("search_query"); got != "cat:cs.AI AND all:transformer" {
		t.Fatalf("search_query = %q", got)
	}
	if got := q.Get("start"); got != "0" {
		t.Fatalf("start = %q", got)
	}
	// url encoding happens at Encode(): space -> +, colon -> %3A
	if enc := q.Encode(); !strings.Contains(enc, "search_query=cat%3Acs.AI+AND+all%3Atransformer") {
		t.Fatalf("encoding: %q", enc)
	}
}

func TestBuildQueryDropsEmptyWhen(t *testing.T) {
	spec := map[string]QueryPart{
		"search_query": {Join: " AND ", Parts: []PartSpec{
			{Value: "cat:{{category}}"},
			{Value: "all:{{terms}}", When: "terms"},
		}},
	}
	q := buildQuery(spec, runtimeVars{"category": "cs.LG", "terms": ""})
	if got := q.Get("search_query"); got != "cat:cs.LG" {
		t.Fatalf("empty terms should drop the all: clause, got %q", got)
	}
}

// --- converter: html -> markdown cleanup ---

func TestConvertHTMLToMarkdown(t *testing.T) {
	const doc = `<html><body>
	  <article class="ltx_document">
	    <h1>Title</h1>
	    <p>Body text.</p>
	    <div class="ltx_bibliography">Refs to drop</div>
	    <math>x^2</math>
	  </article>
	  <footer>site chrome</footer>
	</body></html>`
	md, err := convertHTMLToMarkdown([]byte(doc))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if !strings.Contains(md, "Title") || !strings.Contains(md, "Body text.") {
		t.Fatalf("missing body content: %q", md)
	}
	if strings.Contains(md, "Refs to drop") || strings.Contains(md, "site chrome") || strings.Contains(md, "x^2") {
		t.Fatalf("chrome/bibliography/math not stripped: %q", md)
	}
}

func TestCleanupMarkdownCollapsesBlankLines(t *testing.T) {
	if got := cleanupMarkdown("a\n\n\n\n\nb\n\n"); got != "a\n\nb" {
		t.Fatalf("cleanup = %q", got)
	}
}

// --- safePaperID: SSRF/path-injection guard ---

func TestSafePaperID(t *testing.T) {
	ok := []string{"2401.12345", "cs/0501001", "2401.12345v2"}
	for _, id := range ok {
		if !safePaperID(id) {
			t.Errorf("safePaperID(%q) should be true", id)
		}
	}
	bad := []string{"", "../etc/passwd", "http://evil.com", "a/../b", "id with space", strings.Repeat("x", 65)}
	for _, id := range bad {
		if safePaperID(id) {
			t.Errorf("safePaperID(%q) should be false", id)
		}
	}
}
