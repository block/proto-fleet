package modes

// FullFleet curtails every eligible candidate in scope — no kW target. Unlike
// FixedKw it never returns OutcomeInsufficientLoad: an empty ranked set is a
// valid, vacuously-satisfied result (nothing curtailable == already off), not a
// rejection. Pure logic — no I/O, no time, no shared state.
type FullFleet struct{}

// RequiresDualSignalTelemetry allows FULL_FLEET selection without measured load.
func (FullFleet) RequiresDualSignalTelemetry() bool {
	return false
}

// Select takes all ranked candidates in dispatch order and sums their power.
// Empty input returns OutcomeTargetReached with zero realized reduction.
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
