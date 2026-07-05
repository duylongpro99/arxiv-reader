package llm

// pricing.go is the SINGLE source of token pricing (R4: one dated file, not
// scattered constants). Figures are APPROXIMATE, list-price USD per 1M tokens,
// and drift as providers change pricing — the UI and README label the resulting
// cost an "estimate" and tell the user to check their provider dashboard.
//
// Source: provider public pricing pages, captured 2026-07. Update this map (and
// only this map) when prices change. A model that is absent here yields
// CostKnown=false so the UI simply hides the cost rather than showing a wrong one.

// TokenPricing is the list price for one model, per 1,000,000 tokens.
type TokenPricing struct {
	InputPer1M  float64
	OutputPer1M float64
}

// ModelPricing maps the model strings this project documents/supports to their
// approximate pricing. The default config model (claude-sonnet-4-6) MUST be
// present so the out-of-the-box setup shows a cost estimate.
var ModelPricing = map[string]TokenPricing{
	// Anthropic
	"claude-sonnet-4-6": {InputPer1M: 3.00, OutputPer1M: 15.00}, // default config model
	"claude-opus-4-8":   {InputPer1M: 15.00, OutputPer1M: 75.00},
	"claude-haiku-4-5":  {InputPer1M: 1.00, OutputPer1M: 5.00},
	// OpenAI
	"gpt-4o":      {InputPer1M: 2.50, OutputPer1M: 10.00},
	"gpt-4o-mini": {InputPer1M: 0.15, OutputPer1M: 0.60},
	// Google Gemini
	"gemini-2.0-flash": {InputPer1M: 0.10, OutputPer1M: 0.40},
	"gemini-1.5-pro":   {InputPer1M: 1.25, OutputPer1M: 5.00},
}

// EstimateCost returns the approximate USD cost for the given token counts and
// whether the model was found in the pricing table (false → UI hides the cost).
// The math is (tokens / 1e6) * pricePer1M for input and output, summed.
func EstimateCost(model string, in, out int) (float64, bool) {
	p, ok := ModelPricing[model]
	if !ok {
		return 0, false
	}
	cost := (float64(in)/1_000_000)*p.InputPer1M + (float64(out)/1_000_000)*p.OutputPer1M
	return cost, true
}
