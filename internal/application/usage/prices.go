package usage

import (
	"sort"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// price is a model family's rates in USD per million tokens. Cache writes
// are the 5-minute TTL rate, the one Claude Code uses.
type price struct {
	input, output, cacheWrite, cacheRead float64
	// long, when set, applies to a request whose prompt (input and cache
	// tokens together) exceeds longPromptAbove tokens.
	long *price
}

// longPromptAbove is where a long-prompt price starts.
const longPromptAbove = 100_000

// prices is the harness's price table, per model family, from Anthropic's
// first-party API rates (2026-10). It only serves the estimated cost: on a
// subscription nothing is billed per token. Update it when prices change;
// past records follow, since the cost is computed when a record is read.
var prices = map[string]price{
	"claude-fable-5-1":  {input: 10, output: 50, cacheWrite: 12.5, cacheRead: 0.25},
	"claude-mythos-5-1": {input: 10, output: 50, cacheWrite: 12.5, cacheRead: 0.25},
	"claude-fable-5":    {input: 10, output: 50, cacheWrite: 12.5, cacheRead: 1},
	"claude-mythos-5":   {input: 10, output: 50, cacheWrite: 12.5, cacheRead: 1},

	"claude-opus-5-5": {input: 4, output: 20, cacheWrite: 5, cacheRead: 0.2},
	"claude-opus-5":   {input: 5, output: 25, cacheWrite: 6.25, cacheRead: 0.5},
	"claude-opus-4-8": {input: 5, output: 25, cacheWrite: 6.25, cacheRead: 0.5},
	"claude-opus-4-7": {input: 5, output: 25, cacheWrite: 6.25, cacheRead: 0.5},
	"claude-opus-4-6": {input: 5, output: 25, cacheWrite: 6.25, cacheRead: 0.5},
	"claude-opus-4-5": {input: 5, output: 25, cacheWrite: 6.25, cacheRead: 0.5},
	"claude-opus-4-1": {input: 15, output: 75, cacheWrite: 18.75, cacheRead: 1.5},
	"claude-opus-4":   {input: 15, output: 75, cacheWrite: 18.75, cacheRead: 1.5},
	"claude-3-opus":   {input: 15, output: 75, cacheWrite: 18.75, cacheRead: 1.5},

	"claude-sonnet-5-5": {input: 2, output: 10, cacheWrite: 2.5, cacheRead: 0.2},
	"claude-sonnet-5":   {input: 2, output: 10, cacheWrite: 2.5, cacheRead: 0.2},
	"claude-sonnet-4":   {input: 3, output: 15, cacheWrite: 3.75, cacheRead: 0.3},
	"claude-3-7-sonnet": {input: 3, output: 15, cacheWrite: 3.75, cacheRead: 0.3},
	"claude-3-5-sonnet": {input: 3, output: 15, cacheWrite: 3.75, cacheRead: 0.3},

	"claude-haiku-5-5": {input: 0.1, output: 0.5, cacheWrite: 0.125, cacheRead: 0.01,
		long: &price{input: 0.5, output: 2.5, cacheWrite: 0.625, cacheRead: 0.05}},
	"claude-haiku-4-5": {input: 1, output: 5, cacheWrite: 1.25, cacheRead: 0.1},
	"claude-3-5-haiku": {input: 0.8, output: 4, cacheWrite: 1, cacheRead: 0.08},
	"claude-3-haiku":   {input: 0.25, output: 1.25, cacheWrite: 0.3, cacheRead: 0.03},
}

// families are the table's keys, longest first, so a model id matches its
// most specific family ("claude-opus-4-8" before "claude-opus-4").
var families = func() []string {
	keys := make([]string, 0, len(prices))
	for k := range prices {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	return keys
}()

// priceOf finds the family of a model id as the CLI or a cloud provider may
// spell it: dated ("-20250929", "@20250929"), with a context suffix
// ("[1m]"), or with a provider prefix ("anthropic.", "us.anthropic.").
func priceOf(model string) (price, bool) {
	m := strings.ToLower(model)
	if i := strings.Index(m, "["); i >= 0 {
		m = m[:i]
	}
	if i := strings.LastIndex(m, "anthropic."); i >= 0 {
		m = m[i+len("anthropic."):]
	}
	m = strings.ReplaceAll(m, "@", "-")
	for _, f := range families {
		if m == f || strings.HasPrefix(m, f+"-") {
			return prices[f], true
		}
	}
	return price{}, false
}

// estimatedCost is the record's estimated USD cost, or nil when its model
// has no price.
func estimatedCost(r *domain.UsageRecord) *float64 {
	long := r.InputTokens+r.CacheCreationInputTokens+r.CacheReadInputTokens > longPromptAbove
	usd, ok := costOf(r.Model, long, ports.UsageSums{InputTokens: r.InputTokens, OutputTokens: r.OutputTokens,
		CacheCreationInputTokens: r.CacheCreationInputTokens, CacheReadInputTokens: r.CacheReadInputTokens})
	if !ok {
		return nil
	}
	return &usd
}

// costOf is the estimated USD cost of sums of a model's requests, all with
// long prompts or none; false when the model has no price.
func costOf(model string, long bool, s ports.UsageSums) (float64, bool) {
	p, ok := priceOf(model)
	if !ok {
		return 0, false
	}
	if long && p.long != nil {
		p = *p.long
	}
	return (float64(s.InputTokens)*p.input + float64(s.OutputTokens)*p.output +
		float64(s.CacheCreationInputTokens)*p.cacheWrite + float64(s.CacheReadInputTokens)*p.cacheRead) / 1e6, true
}
