package tracing

import "testing"

func TestEventKindIsTerminal(t *testing.T) {
	terminal := []EventKind{KindRunCompleted, KindRunFailed}
	for _, k := range terminal {
		if !k.IsTerminal() {
			t.Errorf("%q should be terminal", k)
		}
	}
	// Recovery and mid-run events are NOT terminal.
	nonTerminal := []EventKind{
		KindRunRecoveredToSelection, KindDiscoveryStarted, KindSelectionChosen,
		KindLLMExplainerCompleted, KindDecisionAccept,
	}
	for _, k := range nonTerminal {
		if k.IsTerminal() {
			t.Errorf("%q should not be terminal", k)
		}
	}
	// Event.IsTerminal passes through to the kind.
	if !(Event{Kind: KindRunCompleted}).IsTerminal() {
		t.Error("Event.IsTerminal passthrough broken")
	}
}
