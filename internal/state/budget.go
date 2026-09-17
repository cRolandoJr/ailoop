package state

import (
	"errors"
	"fmt"

	"github.com/cRolandoJr/ailoop/internal/llm"
)

// ErrOverBudget means the work has spent what it was allowed to spend.
var ErrOverBudget = errors.New("over budget")

// Budget is the ceiling for one work item.
//
// It is enforced BEFORE each call to a model, not after. Checking afterwards
// reports a number that has already been spent, which is a receipt, not a
// limit.
//
// Tokens are the primary unit because they are what the workflow actually
// measures. Dollars work only when the project states the price of its own
// model: prices differ per provider and change over time, and a figure baked
// into this binary would quietly start lying.
type Budget struct {
	// MaxTokens caps input plus output across the whole work item.
	// Zero means no ceiling.
	MaxTokens int `json:"max_tokens,omitempty"`
	// MaxUSD caps the estimated cost. It needs the two prices below; without
	// them it cannot be evaluated and is reported as unusable rather than
	// silently ignored.
	MaxUSD float64 `json:"max_usd,omitempty"`
	// PriceInPerMTok and PriceOutPerMTok are this model's prices per million
	// tokens, as the project states them.
	PriceInPerMTok  float64 `json:"price_in_per_mtok,omitempty"`
	PriceOutPerMTok float64 `json:"price_out_per_mtok,omitempty"`
}

// Set reports whether any ceiling was configured.
func (b Budget) Set() bool { return b.MaxTokens > 0 || b.MaxUSD > 0 }

// Priced reports whether cost in dollars can be computed at all.
func (b Budget) Priced() bool { return b.PriceInPerMTok > 0 || b.PriceOutPerMTok > 0 }

// CostUSD estimates what a usage has cost, or 0 when no prices are known.
func (b Budget) CostUSD(u llm.Usage) float64 {
	return float64(u.InputTokens)/1e6*b.PriceInPerMTok +
		float64(u.OutputTokens)/1e6*b.PriceOutPerMTok
}

// Check reports whether the work may make another model call.
//
// It refuses on the spend so far: the next call's size is unknown, so the
// honest moment to stop is before making it.
func (b Budget) Check(l *Ledger) error {
	if !b.Set() {
		return nil
	}
	_, total := l.Total()

	if b.MaxTokens > 0 && total.Total() >= b.MaxTokens {
		return fmt.Errorf("%w: %d tokens spent, ceiling is %d",
			ErrOverBudget, total.Total(), b.MaxTokens)
	}

	if b.MaxUSD > 0 {
		if !b.Priced() {
			return fmt.Errorf(
				"%w: a USD ceiling of %.2f was set but this project states no prices. "+
					"Add price_in_per_mtok and price_out_per_mtok, or use max_tokens",
				ErrOverBudget, b.MaxUSD)
		}
		if spent := b.CostUSD(total); spent >= b.MaxUSD {
			mark := ""
			if total.Estimated {
				mark = " (from estimated token counts)"
			}
			return fmt.Errorf("%w: about $%.4f spent, ceiling is $%.2f%s",
				ErrOverBudget, spent, b.MaxUSD, mark)
		}
	}
	return nil
}

// Remaining describes what is left, for display.
func (b Budget) Remaining(l *Ledger) string {
	if !b.Set() {
		return "no ceiling set"
	}
	_, total := l.Total()

	if b.MaxTokens > 0 {
		left := b.MaxTokens - total.Total()
		if left < 0 {
			left = 0
		}
		if b.MaxUSD > 0 && b.Priced() {
			return fmt.Sprintf("%d of %d tokens left, about $%.4f of $%.2f spent",
				left, b.MaxTokens, b.CostUSD(total), b.MaxUSD)
		}
		return fmt.Sprintf("%d of %d tokens left", left, b.MaxTokens)
	}
	if b.Priced() {
		return fmt.Sprintf("about $%.4f of $%.2f spent", b.CostUSD(total), b.MaxUSD)
	}
	return fmt.Sprintf("$%.2f ceiling set, but no prices stated: it cannot be enforced", b.MaxUSD)
}
