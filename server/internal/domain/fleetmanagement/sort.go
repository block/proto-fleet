package fleetmanagement

import (
	"log/slog"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// parseSortConfig converts proto SortConfig slice to domain SortConfig.
// Currently only uses the first element; multi-column sorting reserved for future.
// Returns nil if no sort is specified or if the config is invalid (uses default order).
func parseSortConfig(pbSorts []*commonpb.SortConfig) *interfaces.SortConfig {
	if len(pbSorts) == 0 {
		return nil
	}

	if len(pbSorts) > 1 {
		slog.Warn("multi-column sorting not yet supported, using first sort config only",
			"provided", len(pbSorts))
	}

	pbSort := pbSorts[0]
	if pbSort == nil || pbSort.Field == commonpb.SortField_SORT_FIELD_UNSPECIFIED {
		return nil
	}

	sortConfig := &interfaces.SortConfig{
		Field:     interfaces.SortField(pbSort.Field),
		Direction: interfaces.SortDirection(pbSort.Direction),
	}

	// Default to ASC if direction not specified
	if sortConfig.Direction == interfaces.SortDirectionUnspecified {
		sortConfig.Direction = interfaces.SortDirectionAsc
	}

	// Validate the resulting config
	if !sortConfig.IsValid() {
		slog.Warn("invalid sort config, using default",
			"field", pbSort.Field, "direction", pbSort.Direction)
		return nil
	}

	return sortConfig
}
