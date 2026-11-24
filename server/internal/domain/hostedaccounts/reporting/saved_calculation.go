package reporting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
)

// VerifySavedReviewedCalculation compares the saved reviewed result with the
// full-input baseline. Preparation appends control observations after the sorted
// reviewed interruptions, so only the suffix can be control evidence. The input
// incident category itself is unrestricted and cannot identify that suffix.
// Frozen controls are not re-queried or modified in the saved result.
func VerifySavedReviewedCalculation(ctx context.Context, calculated, saved Result) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("verify saved reviewed calculation: %w", err)
	}
	n := len(calculated.Interruptions)
	if len(saved.Interruptions) < n {
		return fmt.Errorf("saved report is missing reviewed interruptions")
	}
	controls := saved.Interruptions[n:]
	for _, v := range controls {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("verify saved reviewed calculation: %w", err)
		}
		if v.Category != "curtailment_control" {
			return fmt.Errorf("saved report has an unexpected interruption suffix")
		}
	}
	if len(controls) == 0 {
		return compareReviewedResults(calculated, saved)
	}
	saved.Interruptions = saved.Interruptions[:n]
	calculated.Sources = slices.Clone(calculated.Sources)
	for _, v := range controls {
		calculated.Sources = append(calculated.Sources, v.SourceRef)
	}
	calculated.Sources = uniqueStrings(calculated.Sources)
	return compareReviewedResults(calculated, saved)
}

func compareReviewedResults(actual, expected Result) error {
	a, err := json.Marshal(actual)
	if err != nil {
		return fmt.Errorf("encode reviewed calculation: %w", err)
	}
	b, err := json.Marshal(expected)
	if err != nil {
		return fmt.Errorf("encode saved calculation: %w", err)
	}
	if !bytes.Equal(a, b) {
		return fmt.Errorf("reviewed evidence does not reproduce the draft calculation")
	}
	return nil
}
