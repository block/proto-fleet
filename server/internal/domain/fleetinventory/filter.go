package fleetinventory

import (
	"encoding/json"
	"fmt"
	"math"
	"unicode/utf8"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type telemetryRange struct {
	Field        int32    `json:"field"`
	Minimum      *float64 `json:"minimum"`
	Maximum      *float64 `json:"maximum"`
	MinInclusive bool     `json:"min_inclusive"`
	MaxInclusive bool     `json:"max_inclusive"`
}

func listParams(orgID int64, req *pb.ListInventoryRequest, scopes []Scope) (listQueryParams, error) {
	p := listQueryParams{OrgID: orgID}
	invalid := fleeterror.NewInvalidArgumentError("invalid inventory filters")
	if req == nil || len(scopes) == 0 || req.View < 1 || req.View > 3 || req.PageSize < 0 || req.PageSize > 1000 || req.Sort < 0 || req.Sort > 13 || len(req.SiteIds) > 1024 || len(req.Ancestors) > 1024 || len(req.Kinds) > 6 || len(req.TelemetryRanges) > 16 || utf8.RuneCountInString(req.SearchQuery) > 255 {
		return p, invalid
	}
	p.PageLimit = req.PageSize + 1
	if req.PageSize == 0 {
		p.PageLimit = 51
	}
	p.SortField = int32(req.Sort)
	if p.SortField == 0 {
		p.SortField = 1
	}
	p.Descending, p.SearchQuery, p.IncludeUnassigned = req.Descending, req.SearchQuery, req.IncludeUnassigned
	p.SiteIds = append([]int64{}, req.SiteIds...)
	for _, id := range p.SiteIds {
		if id <= 0 {
			return p, invalid
		}
	}
	firstKind := int32(req.View)*2 - 1
	p.Kinds = []int32{firstKind, firstKind + 1}
	if len(req.Kinds) > 0 {
		p.Kinds = nil
		for _, kind := range req.Kinds {
			if int32(kind) != firstKind && int32(kind) != firstKind+1 {
				return p, invalid
			}
			p.Kinds = append(p.Kinds, int32(kind))
		}
	}
	ancestors := make([]map[string]any, 0, len(req.Ancestors))
	for _, ref := range req.Ancestors {
		if ref == nil || ref.Id <= 0 || ref.Kind < 1 || int32(ref.Kind) >= firstKind {
			return p, invalid
		}
		ancestors = append(ancestors, map[string]any{"kind": int32(ref.Kind), "id": ref.Id})
	}
	ranges := make([]telemetryRange, 0, len(req.TelemetryRanges))
	for _, f := range req.TelemetryRanges {
		if f == nil || f.Field < 1 || f.Field > 4 || (f.Min == nil && f.Max == nil) {
			return p, invalid
		}
		r := telemetryRange{Field: int32(f.Field), MinInclusive: f.MinInclusive, MaxInclusive: f.MaxInclusive}
		if f.Min != nil {
			r.Minimum = &f.Min.Value
		}
		if f.Max != nil {
			r.Maximum = &f.Max.Value
		}
		for _, v := range []*float64{r.Minimum, r.Maximum} {
			if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0)) {
				return p, invalid
			}
		}
		if r.Minimum != nil && r.Maximum != nil && (*r.Minimum > *r.Maximum || (*r.Minimum == *r.Maximum && (!r.MinInclusive || !r.MaxInclusive))) {
			return p, invalid
		}
		ranges = append(ranges, r)
	}
	// Empty arrays must encode as [] rather than null for SQL recordset predicates.
	var err error
	if p.Authority, err = marshalScopes(scopes); err != nil {
		return p, err
	}
	if p.Ancestors, err = json.Marshal(ancestors); err != nil {
		return p, fmt.Errorf("encode inventory ancestors: %w", err)
	}
	p.Ranges, err = json.Marshal(ranges)
	if err != nil {
		return p, fmt.Errorf("encode inventory ranges: %w", err)
	}
	return p, nil
}

func marshalScopes(scopes []Scope) ([]byte, error) {
	normalized := make([]Scope, len(scopes))
	for i, s := range scopes {
		normalized[i] = s
		normalized[i].Sites = append([]int64{}, s.Sites...)
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode inventory authority: %w", err)
	}
	return encoded, nil
}
