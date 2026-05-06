package modes

import "fmt"

// FixedKw is the v1 open-loop mode: walk the ranked candidate list,
// accumulating power_w until the target kilowatt reduction is met (or
// declared unreachable). Pure logic — no I/O, no time, no shared state.
type FixedKw struct {
	// TargetKW is the operator-supplied reduction target in kilowatts.
	// Must be > 0; the caller validates the request before constructing.
	TargetKW float64

	// ToleranceKW is the explicitly-accepted undershoot in kilowatts.
	// Default 0 (strict): only OutcomeTargetReached succeeds. Positive
	// values open the OutcomeUndershootTolerated branch where Σpower_w
	// in [target_kw - tolerance_kw, target_kw) takes all candidates.
	// Negative values are not allowed and produce InsufficientLoad with
	// AvailableKW=-1 sentinel; callers should reject before constructing.
	ToleranceKW float64

	// ExclusionSummary is the per-reason exclusion count surfaced in the
	// InsufficientLoadDetail so the UI can render the diagnostic context.
	// The selector populates this from the filter pass before invoking
	// the mode; the mode just forwards it on the rejection branch.
	ExclusionSummary InsufficientLoadDetail
}

// New constructs a FixedKw mode. Target must be > 0; tolerance must be >= 0.
// Returns an error rather than silently coercing so calling code does not
// accidentally promote a 0 target into a no-op selection.
func NewFixedKw(targetKW, toleranceKW float64, summary InsufficientLoadDetail) (*FixedKw, error) {
	if targetKW <= 0 {
		return nil, fmt.Errorf("target_kw must be > 0, got %v", targetKW)
	}
	if toleranceKW < 0 {
		return nil, fmt.Errorf("tolerance_kw must be >= 0, got %v", toleranceKW)
	}
	return &FixedKw{
		TargetKW:         targetKW,
		ToleranceKW:      toleranceKW,
		ExclusionSummary: summary,
	}, nil
}

// Select implements Mode. Three branches per the design:
//
//  1. Target reached: walk candidates accumulating; stop after the first
//     candidate that brings the running sum to or past target_w. Realized W
//     ∈ [target_w, target_w + last_added.power_w].
//  2. Undershoot tolerated: full sum < target_w but >= target_w - tolerance_w.
//     Take all candidates. Only fires when tolerance_kw > 0.
//  3. Insufficient: full sum < target_w - tolerance_w. Empty selection,
//     structured detail. The selector's exclusion counts are forwarded so the
//     caller can render "N miners excluded by candidate_min_power_w=Z W."
func (m *FixedKw) Select(ranked []Candidate) Result {
	const wPerKW = 1000.0
	targetW := m.TargetKW * wPerKW
	toleranceW := m.ToleranceKW * wPerKW

	totalW := 0.0
	for i, c := range ranked {
		totalW += c.PowerW
		if totalW >= targetW {
			selected := make([]Candidate, i+1)
			copy(selected, ranked[:i+1])
			return Result{
				Outcome:            OutcomeTargetReached,
				Selected:           selected,
				RealizedReductionW: totalW,
			}
		}
	}

	// Loop ended without hitting target — totalW is the full-set sum.
	if toleranceW > 0 && totalW >= targetW-toleranceW {
		selected := make([]Candidate, len(ranked))
		copy(selected, ranked)
		return Result{
			Outcome:            OutcomeUndershootTolerated,
			Selected:           selected,
			RealizedReductionW: totalW,
		}
	}

	detail := m.ExclusionSummary
	detail.AvailableKW = totalW / wPerKW
	detail.RequestedKW = m.TargetKW
	detail.ToleranceKW = m.ToleranceKW
	return Result{
		Outcome:            OutcomeInsufficientLoad,
		Selected:           nil,
		RealizedReductionW: totalW,
		InsufficientDetail: &detail,
	}
}
