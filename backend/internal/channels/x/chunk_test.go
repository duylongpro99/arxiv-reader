package x

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestChunkEmpty asserts an empty/whitespace-only body produces no segments —
// Validate relies on this to reject unchunkable input.
func TestChunkEmpty(t *testing.T) {
	for _, body := range []string{"", "   ", "\n\n\t"} {
		if got := chunk(body); len(got) != 0 {
			t.Errorf("chunk(%q) = %v, want empty", body, got)
		}
	}
}

// TestChunkSingleSegment asserts a short body fits in one tweet with NO
// counter suffix — a lone tweet shouldn't carry a redundant "(1/1)".
func TestChunkSingleSegment(t *testing.T) {
	body := "A short brief that easily fits in one tweet."
	got := chunk(body)
	if len(got) != 1 {
		t.Fatalf("chunk() = %d segments, want 1: %v", len(got), got)
	}
	if got[0] != body {
		t.Errorf("chunk()[0] = %q, want unchanged %q (no counter for a single tweet)", got[0], body)
	}
}

// TestChunkExactly280 asserts a body of exactly maxTweetLen chars (no
// sentence terminator to force a split) still yields exactly one segment,
// unmodified — the boundary case for "fits in one tweet".
func TestChunkExactly280(t *testing.T) {
	body := strings.Repeat("a", maxTweetLen)
	got := chunk(body)
	if len(got) != 1 || got[0] != body {
		t.Fatalf("chunk(280 chars) = %v, want 1 unchanged segment", got)
	}
}

// TestChunkMultiSegmentCounters drives a body long enough to require several
// tweets and asserts EVERY segment: (a) is <= maxTweetLen, (b) carries a
// " (i/N)" suffix with i matching its position and N matching the total
// count, and (c) never splits a word (every space-delimited token in the
// stripped segment appears verbatim in the original word stream).
func TestChunkMultiSegmentCounters(t *testing.T) {
	sentence := "The quick brown fox jumps over the lazy dog again and again."
	body := strings.Repeat(sentence+" ", 40) // long enough to force multiple tweets
	segments := chunk(body)

	if len(segments) < 2 {
		t.Fatalf("chunk() = %d segments, want >= 2 for a long body", len(segments))
	}

	counterRe := regexp.MustCompile(`^(.*) \((\d+)/(\d+)\)$`)
	n := len(segments)
	var reconstructedWords []string
	for idx, seg := range segments {
		if len(seg) > maxTweetLen {
			t.Errorf("segment %d length = %d, exceeds %d: %q", idx, len(seg), maxTweetLen, seg)
		}
		m := counterRe.FindStringSubmatch(seg)
		if m == nil {
			t.Fatalf("segment %d = %q missing ' (i/N)' counter", idx, seg)
		}
		i, _ := strconv.Atoi(m[2])
		gotN, _ := strconv.Atoi(m[3])
		if i != idx+1 {
			t.Errorf("segment %d counter i = %d, want %d", idx, i, idx+1)
		}
		if gotN != n {
			t.Errorf("segment %d counter N = %d, want %d", idx, gotN, n)
		}
		reconstructedWords = append(reconstructedWords, strings.Fields(m[1])...)
	}

	originalWords := strings.Fields(body)
	if len(reconstructedWords) != len(originalWords) {
		t.Fatalf("word count after stripping counters = %d, want %d (no data loss/duplication)",
			len(reconstructedWords), len(originalWords))
	}
	for i, w := range originalWords {
		if reconstructedWords[i] != w {
			t.Errorf("word %d = %q, want %q (a word must never be split mid-word)", i, reconstructedWords[i], w)
		}
	}
}

// TestChunkCounterDigitBoundary forces the segment count to cross a digit
// boundary (9 -> 10+ segments), the case where the counter's own width grows
// mid-computation. Every resulting segment must still fit within
// maxTweetLen once its counter is appended.
func TestChunkCounterDigitBoundary(t *testing.T) {
	// Each "word" below is exactly one budget-sized chunk once counters are
	// reserved for, so repeating it forces roughly one segment per
	// repetition — comfortably crossing the 9->10 boundary.
	word := strings.Repeat("w", 250)
	body := strings.Repeat(word+" ", 12)

	segments := chunk(body)
	if len(segments) < 10 {
		t.Fatalf("chunk() = %d segments, want >= 10 to exercise the digit-width boundary", len(segments))
	}
	for idx, seg := range segments {
		if len(seg) > maxTweetLen {
			t.Errorf("segment %d length = %d, exceeds %d: %q", idx, len(seg), maxTweetLen, seg)
		}
	}
	wantSuffix := fmt.Sprintf(" (%d/%d)", len(segments), len(segments))
	if !strings.HasSuffix(segments[len(segments)-1], wantSuffix) {
		t.Errorf("last segment = %q, want suffix %q", segments[len(segments)-1], wantSuffix)
	}
}

// TestChunkOverlongWord asserts a single word far longer than maxTweetLen is
// hard-split into budget-sized (accounting for its counter) pieces — the
// only path allowed to break mid-word — and rejoining every hard-split
// fragment reproduces the original word exactly.
func TestChunkOverlongWord(t *testing.T) {
	longWord := strings.Repeat("x", 700) // needs 3+ tweets on its own
	segments := chunk(longWord)

	if len(segments) < 3 {
		t.Fatalf("chunk() = %d segments, want >= 3 for a 700-char word", len(segments))
	}
	counterRe := regexp.MustCompile(`^(.*) \(\d+/\d+\)$`)
	var rebuilt strings.Builder
	for idx, seg := range segments {
		if len(seg) > maxTweetLen {
			t.Errorf("segment %d length = %d, exceeds %d", idx, len(seg), maxTweetLen)
		}
		m := counterRe.FindStringSubmatch(seg)
		if m == nil {
			t.Fatalf("segment %d = %q missing counter", idx, seg)
		}
		rebuilt.WriteString(m[1])
	}
	if rebuilt.String() != longWord {
		t.Errorf("rejoined hard-split fragments = %d chars, want the original %d-char word intact", rebuilt.Len(), len(longWord))
	}
}

// TestChunkMultiParagraph asserts a segment's text never straddles two
// paragraphs: each output segment's stripped body must be a substring of
// exactly one input paragraph.
func TestChunkMultiParagraph(t *testing.T) {
	para1 := "First paragraph with a couple of short sentences. Still short."
	para2 := "Second paragraph, entirely distinct content that follows a blank line."
	para3 := "Third and final paragraph rounding things out nicely."
	body := para1 + "\n\n" + para2 + "\n\n" + para3

	segments := chunk(body)
	counterRe := regexp.MustCompile(`^(.*) \(\d+/\d+\)$`)
	paragraphs := []string{para1, para2, para3}

	for idx, seg := range segments {
		text := seg
		if m := counterRe.FindStringSubmatch(seg); m != nil {
			text = m[1]
		}
		matched := false
		for _, p := range paragraphs {
			if strings.Contains(p, text) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("segment %d = %q is not a substring of any single input paragraph (straddles a paragraph boundary)", idx, text)
		}
	}
}

// TestChunkNeverExceedsBudgetAcrossSizes is a small sweep over body lengths
// to catch any off-by-one in the counter-width fixed point, without relying
// on one hand-picked length.
func TestChunkNeverExceedsBudgetAcrossSizes(t *testing.T) {
	unit := "Lorem ipsum dolor sit amet consectetur adipiscing elit sed do. "
	for reps := 1; reps <= 60; reps++ {
		body := strings.Repeat(unit, reps)
		segments := chunk(body)
		for idx, seg := range segments {
			if len(seg) > maxTweetLen {
				t.Fatalf("reps=%d segment %d length = %d, exceeds %d: %q", reps, idx, len(seg), maxTweetLen, seg)
			}
		}
	}
}
