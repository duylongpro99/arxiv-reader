package resource

import "testing"

// sampleFeed mirrors the old discovery_test fixture: two entries with the
// newline-wrapped title/summary arXiv actually emits, one PDF link, one derived.
const sampleFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>http://arxiv.org/abs/2401.12345v2</id>
    <title>A Study of
      Large Language Models</title>
    <summary>  We investigate
      scaling behaviour of LLMs.  </summary>
    <published>  2024-01-20T10:30:00Z  </published>
    <author><name>Ada Lovelace</name></author>
    <author><name>Alan Turing</name></author>
    <link href="http://arxiv.org/abs/2401.12345v2" rel="alternate" type="text/html"/>
    <link href="http://arxiv.org/pdf/2401.12345v2" rel="related" type="application/pdf"/>
  </entry>
  <entry>
    <id>http://arxiv.org/abs/2402.00001v1</id>
    <title>Second Paper</title>
    <summary>Another abstract.</summary>
    <published>2024-02-01T08:00:00Z</published>
    <author><name>Grace Hopper</name></author>
  </entry>
</feed>`

// arxivResponseSpec reproduces the arXiv field mapping the YAML declares, so the
// normalizer is tested in isolation (no HTTP, no loader).
func arxivResponseSpec() ResponseSpec {
	return ResponseSpec{
		Format: "atom-xml",
		Items:  "feed.entry",
		Fields: map[string]FieldMap{
			"id":        {Derive: "arxiv-id"},
			"title":     {Path: "title", Transforms: []TransformSpec{{Name: "normalize"}}},
			"abstract":  {Path: "summary", Transforms: []TransformSpec{{Name: "normalize"}}},
			"authors":   {Path: "author.name", Multi: true, Transforms: []TransformSpec{{Name: "normalize"}}},
			"published": {Path: "published", Transforms: []TransformSpec{{Name: "trim"}}},
			"pdfUrl":    {Derive: "arxiv-pdf-url"},
		},
		Require: []string{"id", "title"},
	}
}

func normalizeFeed(t *testing.T, feed string) []papersResult {
	t.Helper()
	root, err := decodeAtomXML([]byte(feed))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	cr, err := compileResponse(arxivResponseSpec(), "arxiv")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	papers := cr.normalize(root)
	out := make([]papersResult, len(papers))
	for i, p := range papers {
		out[i] = papersResult{p.ID, p.Title, p.Abstract, p.Authors, p.PDFURL, p.Published, p.Source}
	}
	return out
}

type papersResult struct {
	ID, Title, Abstract string
	Authors             []string
	PDFURL, Published   string
	Source              string
}

func TestNormalizeHappyPath(t *testing.T) {
	got := normalizeFeed(t, sampleFeed)
	if len(got) != 2 {
		t.Fatalf("want 2 papers, got %d", len(got))
	}
	p := got[0]
	if p.ID != "2401.12345" {
		t.Errorf("id = %q", p.ID)
	}
	if p.Title != "A Study of Large Language Models" {
		t.Errorf("title not normalized: %q", p.Title)
	}
	if p.Abstract != "We investigate scaling behaviour of LLMs." {
		t.Errorf("abstract not normalized: %q", p.Abstract)
	}
	if len(p.Authors) != 2 || p.Authors[0] != "Ada Lovelace" {
		t.Errorf("authors: %#v", p.Authors)
	}
	if p.Published != "2024-01-20T10:30:00Z" {
		t.Errorf("published not trimmed: %q", p.Published)
	}
	if p.PDFURL != "http://arxiv.org/pdf/2401.12345v2" {
		t.Errorf("pdfUrl (explicit link): %q", p.PDFURL)
	}
	if p.Source != "arxiv" {
		t.Errorf("source = %q, want arxiv", p.Source)
	}
	// second entry has no pdf link → derived from stripped id
	if got[1].PDFURL != "https://arxiv.org/pdf/2402.00001" {
		t.Errorf("derived pdfUrl: %q", got[1].PDFURL)
	}
}

func TestNormalizeEmptyFeed(t *testing.T) {
	const empty = `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"></feed>`
	if got := normalizeFeed(t, empty); len(got) != 0 {
		t.Fatalf("empty feed should yield 0 papers, got %d", len(got))
	}
}

// F10: an item missing a required field is SKIPPED (not fatal to the batch).
func TestNormalizeSkipsMissingRequired(t *testing.T) {
	const feed = `<feed xmlns="http://www.w3.org/2005/Atom">
	  <entry><id>http://arxiv.org/abs/2401.99999v1</id></entry>
	  <entry><id>http://arxiv.org/abs/2401.00002v1</id><title>Has Title</title></entry>
	</feed>`
	got := normalizeFeed(t, feed)
	if len(got) != 1 || got[0].ID != "2401.00002" {
		t.Fatalf("expected only the entry with a title, got %#v", got)
	}
}

// F20: an empty <author><name/> is dropped, not surfaced as an empty author.
func TestNormalizeDropsEmptyAuthor(t *testing.T) {
	const feed = `<feed xmlns="http://www.w3.org/2005/Atom">
	  <entry>
	    <id>http://arxiv.org/abs/2401.00003v1</id>
	    <title>T</title>
	    <author><name></name></author>
	    <author><name>Real Author</name></author>
	  </entry>
	</feed>`
	got := normalizeFeed(t, feed)
	if len(got) != 1 || len(got[0].Authors) != 1 || got[0].Authors[0] != "Real Author" {
		t.Fatalf("empty author not dropped: %#v", got)
	}
}

// F2: old-style ids keep their embedded slash (afterLast("/") would mangle them).
func TestNormalizeOldStyleID(t *testing.T) {
	const feed = `<feed xmlns="http://www.w3.org/2005/Atom">
	  <entry><id>http://arxiv.org/abs/cs/0501001v1</id><title>Old</title></entry>
	</feed>`
	got := normalizeFeed(t, feed)
	if len(got) != 1 || got[0].ID != "cs/0501001" {
		t.Fatalf("old-style id: %#v", got)
	}
	if got[0].PDFURL != "https://arxiv.org/pdf/cs/0501001" {
		t.Errorf("old-style derived pdfUrl: %q", got[0].PDFURL)
	}
}

// F3: a type=pdf link WITHOUT /pdf/ is ignored; the canonical url is derived.
func TestNormalizePDFLinkWithoutPdfPath(t *testing.T) {
	const feed = `<feed xmlns="http://www.w3.org/2005/Atom">
	  <entry>
	    <id>http://arxiv.org/abs/2401.55555v1</id><title>T</title>
	    <link href="http://arxiv.org/abs/2401.55555" rel="related" type="application/pdf"/>
	  </entry>
	</feed>`
	got := normalizeFeed(t, feed)
	if got[0].PDFURL != "https://arxiv.org/pdf/2401.55555" {
		t.Errorf("should derive, not use non-/pdf/ link: %q", got[0].PDFURL)
	}
}
