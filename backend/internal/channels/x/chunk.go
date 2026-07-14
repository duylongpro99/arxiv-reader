package x

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const (
	// maxTweetLen is X's plain-text character cap. Every returned segment
	// (body text + its " (i/N)" counter, when present) must be <= this.
	maxTweetLen = 280
	// maxCounterFixups bounds the fixed-point loop in chunk() that re-wraps
	// the body as the counter's digit width grows (e.g. 9->10 segments
	// widens " (9/9)" to " (10/10)"). Segment count only grows and digit
	// width only grows as budget shrinks, so this converges in 1-2 passes
	// for any realistic body; the bound only guards against a pathological
	// runaway and is never expected to be hit.
	maxCounterFixups = 8
)

// paraSplitRe splits body text on blank-line boundaries (one or more blank
// lines), the strongest structural signal in markdown/plain-text input.
var paraSplitRe = regexp.MustCompile(`\n\s*\n+`)

// chunk deterministically splits body into a thread of segments, each
// <=maxTweetLen chars, never splitting a word. When more than one segment is
// produced, every segment gets a trailing " (i/N)" counter (single-segment
// threads stay counter-free — a lone tweet doesn't need "(1/1)").
//
// Boundary preference: a segment's text never straddles a paragraph break
// (paragraphs are packed independently); within a paragraph, whole sentences
// are packed greedily up to the budget (sentence boundary preferred over
// mid-sentence); only a single sentence that alone exceeds the budget is
// broken at word boundaries; only a single word that alone exceeds the
// budget is hard-split (never mid-word otherwise).
func chunk(body string) []string {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}

	segments := wrap(body, maxTweetLen)
	if len(segments) <= 1 {
		return segments
	}

	// Reserve room for the widest possible counter up front instead of
	// reactively re-splitting overflowing segments. counterWidth is computed
	// as len(" (N/N)") — the widest any " (i/N)" can be for i in [1,N],
	// since digit-count is monotonic non-decreasing in i. Wrapping at
	// (280 - counterWidth) therefore GUARANTEES every segment+counter fits
	// in 280 by construction: no segment can ever "almost fit" and need a
	// further split once counters are appended. Because shrinking the
	// budget can only grow the segment count (never shrink it), and a
	// bigger count can only grow-or-hold the digit width, this loop is a
	// monotone fixed point that converges quickly.
	n := len(segments)
	for i := 0; i < maxCounterFixups; i++ {
		budget := maxTweetLen - len(counterText(n, n))
		segments = wrap(body, budget)
		if len(segments) == n {
			break
		}
		n = len(segments)
	}

	out := make([]string, len(segments))
	for i, seg := range segments {
		out[i] = seg + counterText(i+1, len(segments))
	}
	return out
}

// counterText renders the " (i/N)" suffix appended to each thread segment.
func counterText(i, n int) string {
	return fmt.Sprintf(" (%d/%d)", i, n)
}

// wrap packs body into segments <= budget chars, never straddling a
// paragraph boundary.
func wrap(body string, budget int) []string {
	if budget < 1 {
		budget = 1 // guard: keeps hardSplit terminating even under a pathological budget
	}
	var out []string
	for _, para := range splitParagraphs(body) {
		out = append(out, wrapParagraph(para, budget)...)
	}
	return out
}

// splitParagraphs breaks body on blank lines and collapses each paragraph's
// internal whitespace (including single line breaks) to single spaces —
// markdown line-wrapping carries no meaning once flattened into a tweet.
func splitParagraphs(body string) []string {
	raw := paraSplitRe.Split(strings.TrimSpace(body), -1)
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		collapsed := strings.Join(strings.Fields(p), " ")
		if collapsed != "" {
			out = append(out, collapsed)
		}
	}
	return out
}

// wrapParagraph greedily packs whole sentences into segments up to budget.
// A sentence is only broken (at word boundaries) when it alone exceeds
// budget — sentence boundaries are preferred over word boundaries.
func wrapParagraph(para string, budget int) []string {
	var out []string
	var cur string
	flush := func() {
		if cur != "" {
			out = append(out, cur)
			cur = ""
		}
	}
	for _, sent := range splitSentences(para) {
		candidate := sent
		if cur != "" {
			candidate = cur + " " + sent
		}
		if len(candidate) <= budget {
			cur = candidate
			continue
		}
		// candidate overflows: whatever is already packed stands on its own.
		flush()
		if len(sent) <= budget {
			cur = sent
			continue
		}
		// The sentence alone still exceeds budget: fall back to word packing.
		out = append(out, wrapWords(sent, budget)...)
	}
	flush()
	return out
}

// sentenceEnders are the punctuation runes wrapParagraph treats as
// sentence-boundary candidates.
var sentenceEnders = map[rune]bool{'.': true, '!': true, '?': true}

// splitSentences performs a lightweight, deterministic sentence split: a run
// ends when a sentence-ending punctuation mark is followed by whitespace (or
// end of string). This is intentionally simple — no abbreviation/NLP
// handling — matching the "deterministic, no LLM" requirement; the rare
// false split (e.g. "Dr. Smith") just yields one extra short segment, never
// incorrect output.
func splitSentences(para string) []string {
	var sentences []string
	var cur strings.Builder
	runes := []rune(para)
	for i, r := range runes {
		cur.WriteRune(r)
		if sentenceEnders[r] && (i+1 >= len(runes) || unicode.IsSpace(runes[i+1])) {
			sentences = append(sentences, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		sentences = append(sentences, strings.TrimSpace(cur.String()))
	}
	return sentences
}

// wrapWords greedily packs words into segments up to budget; a single word
// longer than budget is hard-split (the only case that ever breaks mid-word).
func wrapWords(sent string, budget int) []string {
	var out []string
	var cur string
	flush := func() {
		if cur != "" {
			out = append(out, cur)
			cur = ""
		}
	}
	for _, w := range strings.Fields(sent) {
		if len(w) > budget {
			flush()
			out = append(out, hardSplit(w, budget)...)
			continue
		}
		candidate := w
		if cur != "" {
			candidate = cur + " " + w
		}
		if len(candidate) <= budget {
			cur = candidate
		} else {
			flush()
			cur = w
		}
	}
	flush()
	return out
}

// hardSplit is the last resort: a single word longer than budget, chopped
// into budget-sized (rune-safe) pieces. Only reached for pathological input
// (e.g. a URL or hashtag longer than 280 chars) — always terminates because
// wrap() clamps budget >= 1.
func hardSplit(word string, budget int) []string {
	var out []string
	runes := []rune(word)
	for len(runes) > 0 {
		n := budget
		if n > len(runes) {
			n = len(runes)
		}
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}
