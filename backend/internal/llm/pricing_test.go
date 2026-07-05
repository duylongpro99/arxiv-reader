package llm

import (
	"math"
	"testing"
)

func TestEstimateCostKnownModel(t *testing.T) {
	// claude-sonnet-4-6: $3/1M in, $15/1M out.
	// 1,000,000 in + 200,000 out → 3.00 + 3.00 = 6.00.
	cost, known := EstimateCost("claude-sonnet-4-6", 1_000_000, 200_000)
	if !known {
		t.Fatal("default model must be in the pricing table")
	}
	if math.Abs(cost-6.00) > 1e-9 {
		t.Fatalf("cost = %v, want 6.00", cost)
	}
}

func TestEstimateCostUnknownModelHidesCost(t *testing.T) {
	cost, known := EstimateCost("some-unlisted-model", 1000, 1000)
	if known {
		t.Fatal("unknown model must report CostKnown=false")
	}
	if cost != 0 {
		t.Fatalf("unknown model cost = %v, want 0", cost)
	}
}

func TestEstimateCostZeroTokens(t *testing.T) {
	cost, known := EstimateCost("gpt-4o", 0, 0)
	if !known || cost != 0 {
		t.Fatalf("zero tokens: cost=%v known=%v, want 0/true", cost, known)
	}
}
