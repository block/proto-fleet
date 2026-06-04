package modes

// FullFleet curtails every eligible candidate in scope — no kW target. Unlike
// FixedKw it never returns OutcomeInsufficientLoad: an empty ranked set is a
// valid, vacuously-satisfied result (nothing curtailable == already off), not a
// rejection. Pure logic — no I/O, no time, no shared state.
type FullFleet struct{}

// Select implements Mode: take all ranked candidates in dispatch order and sum
// their power. Empty input yields an empty Selected with OutcomeTargetReached
// (RealizedReductionW 0) — never OutcomeInsufficientLoad.
func (FullFleet) Select(ranked []Candidate) Result {
	selected := make([]Candidate, len(ranked))
	copy(selected, ranked)

	var totalW float64
	for _, c := range ranked {
		totalW += c.PowerW
	}
	return Result{
		Outcome:            OutcomeTargetReached,
		Selected:           selected,
		RealizedReductionW: totalW,
	}
}
