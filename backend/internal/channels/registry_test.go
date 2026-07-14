package channels

import (
	"strings"
	"testing"

	"github.com/maritime-ds/arxiv-reader/internal/config"
)

// TestNewChannelUnknownID asserts the registry stub returns a descriptive
// error (never a nil Channel with a nil error) for any id — every case is
// still a commented placeholder awaiting a later phase's channel package.
func TestNewChannelUnknownID(t *testing.T) {
	for _, id := range []string{"devto", "x", "mastodon", ""} {
		ch, err := NewChannel(id, &config.Config{})
		if ch != nil {
			t.Fatalf("NewChannel(%q) returned non-nil Channel %v before any case is implemented", id, ch)
		}
		if err == nil {
			t.Fatalf("NewChannel(%q) returned nil error, want a descriptive error", id)
		}
		if !strings.Contains(err.Error(), id) {
			t.Errorf("NewChannel(%q) error %q does not name the requested id", id, err.Error())
		}
	}
}
