package reporting

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"time"
	"unicode/utf8"
)

// FactKind identifies a register, not a globally unique fact identity. IDs are
// unique within a register and account; different registers may reuse an ID.
type FactKind string

const (
	FactOrder           FactKind = "order"
	FactCoverage        FactKind = "coverage"
	FactModuleCoverage  FactKind = "module_coverage"
	FactMeter           FactKind = "meter"
	FactFee             FactKind = "fee"
	FactIncident        FactKind = "incident"
	FactHashrate        FactKind = "hashrate"
	FactPoolObservation FactKind = "pool_observation"
	FactObligation      FactKind = "obligation"
	FactExternalResult  FactKind = "external_result"
)

// Valid reports whether kind names a supported register.
func (kind FactKind) Valid() bool {
	switch kind {
	case FactOrder, FactCoverage, FactModuleCoverage, FactMeter, FactFee, FactIncident, FactHashrate, FactPoolObservation, FactObligation, FactExternalResult:
		return true
	default:
		return false
	}
}

// Fact is a server-derived projection of one reviewed input. The original
// payload is retained without prorating values; the fields beside it are for
// indexed selection and must never be supplied separately by an API client.
// A zero End is an open interval. Pool observations use Start as source time;
// their receipt time is independent and remains in ReceivedAt.
type Fact struct {
	Kind                 FactKind
	ID                   string
	Start                time.Time
	End                  time.Time
	ReceivedAt           time.Time
	SiteID               int64
	ContainerID          int64
	AssetKind            int32
	AssetID              int64
	OrderID              string
	AllocationIntervalID int64
	EpisodeID            string
	MeterID              string
	BoardCoverageIDs     []string
	Payload              json.RawMessage
}

// EncodeFact derives index fields from the typed value. It does not replace
// Inputs.ValidateContext or account-specific authorization and attribution.
func EncodeFact(value any) (Fact, error) {
	var f Fact
	switch v := value.(type) {
	case Order:
		f.Kind, f.ID, f.Start, f.End = FactOrder, v.ID, v.Start, v.End
	case CoverageSegment:
		f.AllocationIntervalID = v.AllocationIntervalID
		f.Kind, f.ID, f.Start, f.End = FactCoverage, v.ID, v.Start, v.End
		f.SiteID, f.ContainerID, f.OrderID, f.AssetKind, f.AssetID = v.SiteID, v.ContainerID, v.OrderID, v.Kind, v.AssetID
	case ModuleCoverageSegment:
		f.Kind, f.ID, f.Start, f.End = FactModuleCoverage, v.ID, v.Start, v.End
		f.SiteID, f.ContainerID, f.OrderID, f.AssetKind, f.AssetID = v.SiteID, v.ContainerID, v.OrderID, AssetKindModule, v.ModuleID
		f.BoardCoverageIDs = slices.Clone(v.BoardCoverageIDs)
	case MeterInterval:
		f.Kind, f.ID, f.Start, f.End = FactMeter, v.ID, v.Start, v.End
		f.SiteID, f.ContainerID, f.OrderID, f.MeterID = v.SiteID, v.ContainerID, v.OrderID, v.MeterID
	case FeeLine:
		f.Kind, f.ID, f.Start, f.End = FactFee, v.ID, v.Start, v.End
		f.SiteID, f.ContainerID, f.OrderID = v.SiteID, v.ContainerID, v.OrderID
	case Incident:
		f.Kind, f.ID, f.Start, f.End = FactIncident, v.ID, v.Start, v.End
		f.SiteID, f.ContainerID, f.OrderID = v.SiteID, v.ContainerID, v.OrderID
	case HashrateInterval:
		f.Kind, f.ID, f.Start, f.End = FactHashrate, v.ID, v.Start, v.End
		f.SiteID, f.ContainerID, f.OrderID, f.AssetKind, f.AssetID = v.SiteID, v.ContainerID, v.OrderID, v.Kind, v.AssetID
	case PoolObservation:
		f.Kind, f.ID, f.Start, f.ReceivedAt = FactPoolObservation, v.ID, v.SourceAt, v.ReceivedAt
		f.AssetKind, f.AssetID = v.Kind, v.AssetID
	case ObligationInterval:
		f.EpisodeID = v.EpisodeID
		f.Kind, f.ID, f.Start, f.End, f.ReceivedAt = FactObligation, v.ID, v.Start, v.End, v.ReceivedAt
		f.SiteID, f.ContainerID, f.OrderID, f.AssetKind, f.AssetID = v.SiteID, v.ContainerID, v.OrderID, v.Kind, v.AssetID
	case ContractResult:
		f.Kind, f.ID, f.Start, f.End = FactExternalResult, v.ID, v.Start, v.End
		f.SiteID, f.ContainerID, f.OrderID = v.SiteID, v.ContainerID, v.OrderID
	default:
		return Fact{}, fmt.Errorf("unsupported reporting fact type %T", value)
	}
	if !utf8.ValidString(f.ID) || !text(f.ID) || f.Start.IsZero() {
		return Fact{}, fmt.Errorf("invalid %s identity or start", f.Kind)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return Fact{}, fmt.Errorf("encode %s fact: %w", f.Kind, err)
	}
	f.Payload = payload
	return f, nil
}

// DecodeFact reads a payload in its declared register, then derives its index
// fields again. Unknown fields, trailing values and null are errors rather than
// silent evidence loss. The returned value is the corresponding typed struct.
func DecodeFact(kind FactKind, payload []byte) (Fact, any, error) {
	switch kind {
	case FactOrder:
		return decodeFact[Order](payload)
	case FactCoverage:
		return decodeFact[CoverageSegment](payload)
	case FactModuleCoverage:
		return decodeFact[ModuleCoverageSegment](payload)
	case FactMeter:
		return decodeFact[MeterInterval](payload)
	case FactFee:
		return decodeFact[FeeLine](payload)
	case FactIncident:
		return decodeFact[Incident](payload)
	case FactHashrate:
		return decodeFact[HashrateInterval](payload)
	case FactPoolObservation:
		return decodeFact[PoolObservation](payload)
	case FactObligation:
		return decodeFact[ObligationInterval](payload)
	case FactExternalResult:
		return decodeFact[ContractResult](payload)
	default:
		return Fact{}, nil, fmt.Errorf("unknown reporting fact kind %q", kind)
	}
}

func decodeFact[T any](payload []byte) (Fact, any, error) {
	if !utf8.Valid(payload) {
		return Fact{}, nil, fmt.Errorf("reporting fact must contain valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var value *T
	if err := decoder.Decode(&value); err != nil {
		return Fact{}, nil, fmt.Errorf("decode reporting fact: %w", err)
	}
	if value == nil {
		return Fact{}, nil, fmt.Errorf("reporting fact must be an object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Fact{}, nil, fmt.Errorf("reporting fact contains trailing data")
		}
		return Fact{}, nil, fmt.Errorf("decode trailing reporting fact data: %w", err)
	}
	fact, err := EncodeFact(*value)
	if err != nil {
		return Fact{}, nil, err
	}
	return fact, *value, nil
}
