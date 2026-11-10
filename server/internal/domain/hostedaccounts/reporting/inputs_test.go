package reporting

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInputValidationPreservesUnknownAndRejectsUnsupportedAttribution(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	inputs := Inputs{Configuration: Configuration{Timezone: "UTC", ReportingYearMonth: 1, ReportingYearDay: 1}, Orders: []Order{{ID: "order", Name: "Order", Start: start, SourceRef: "signed order"}}, Coverage: []CoverageSegment{{ID: "coverage", AllocationIntervalID: 1, Kind: 6, AssetID: 1, SiteID: 1, ContainerID: 1, OrderID: "order", Start: start, CapacityTHS: "100", SourceRef: "commissioning"}}}
	require.NoError(t, inputs.Validate())
	require.False(t, inputs.Configuration.Ready("G1"), "absence is not approved")
	inputs.Coverage = append(inputs.Coverage, inputs.Coverage[0])
	inputs.Coverage[1].ID = "overlap"
	require.ErrorContains(t, inputs.Validate(), "overlapping coverage")
}

func TestInputValidationRejectsImpreciseOrUnattributedFinancialEvidence(t *testing.T) {
	in := Inputs{Configuration: Configuration{Timezone: "UTC", ReportingYearMonth: 1, ReportingYearDay: 1}}
	in.Fees = []FeeLine{{ID: "fee", Amount: "NaN", Currency: "USD", SourceRef: "finance", OrderID: "missing", ContainerID: 1, SiteID: 1, Start: time.Now(), End: time.Now().Add(time.Hour)}}
	require.Error(t, in.Validate())
	in.Fees = nil
	in.Configuration.Timezone = "not/a/timezone"
	require.ErrorContains(t, in.Validate(), "timezone")
	for _, text := range []string{"NaN", "Inf", "1e3", "1/3", " 1", "-1"} {
		_, err := Decimal(text, false)
		require.Error(t, err, text)
	}
	value, err := Decimal("0.10", false)
	require.NoError(t, err)
	require.Equal(t, "1/10", value.RatString())
}

func TestPeriodUsesExplicitContractCalendar(t *testing.T) {
	p := Period{Kind: "monthly", Start: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), AsOf: time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC), Timezone: "UTC"}
	require.NoError(t, p.Validate())
	p.End = p.End.Add(-time.Second)
	require.Error(t, p.Validate())
}

func TestReviewedSharedMeterAllocationsCannotDoubleConsumption(t *testing.T) {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	in := Inputs{Configuration: Configuration{Timezone: "UTC", ReportingYearMonth: 1, ReportingYearDay: 31, SharedAllocationRef: "signed equal split"}, Orders: []Order{{ID: "a", Name: "A", Start: at, SourceRef: "order a"}, {ID: "b", Name: "B", Start: at, SourceRef: "order b"}}, Meters: []MeterInterval{{ID: "a", MeterID: "meter", SourceID: "meter-source", SiteID: 1, ContainerID: 1, OrderID: "a", Start: at, End: at.Add(time.Hour), KWh: "100", Quality: "measured", SourceRef: "meter reading", AllocationFraction: "0.5"}, {ID: "b", MeterID: "meter", SourceID: "meter-source", SiteID: 1, ContainerID: 1, OrderID: "b", Start: at, End: at.Add(time.Hour), KWh: "100", Quality: "measured", SourceRef: "meter reading", AllocationFraction: "0.5"}}}
	require.NoError(t, in.Validate())
	in.Meters[1].AllocationFraction = "0.6"
	require.ErrorContains(t, in.Validate(), "exceeds one")
}

func TestInputCalendarAndCurrencyAreExplicit(t *testing.T) {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	in := Inputs{Configuration: Configuration{Timezone: "UTC", ReportingYearMonth: 1, ReportingYearDay: 1}, Orders: []Order{{ID: "order", Name: "Order", Start: at, SourceRef: "signed"}}}
	in.Configuration.Timezone = "Local"
	require.ErrorContains(t, in.Validate(), "timezone")
	p := Period{Kind: "ad_hoc", Start: at, End: at.Add(time.Hour), AsOf: at.Add(time.Hour), Timezone: "Local"}
	require.ErrorContains(t, p.Validate(), "timezone")
	in.Configuration.Timezone = "Africa/Nairobi"
	in.Fees = []FeeLine{{ID: "fee", SiteID: 1, ContainerID: 1, OrderID: "order", Start: at, End: at.Add(time.Hour), Amount: "1.25", Currency: "USD", SourceRef: "invoice"}}
	for _, currency := range []string{"123", "U$D", "US ", "usd", "ÜS"} {
		in.Fees[0].Currency = currency
		require.ErrorContains(t, in.Validate(), "fee attribution", currency)
	}
	in.Fees[0].Currency = "KES"
	other := in.Fees[0]
	other.ID = "additional-charge"
	in.Fees = append(in.Fees, other)
	require.NoError(t, in.Validate(), "separate overlapping accrued charges remain valid")
}

func meterValidationInputs() Inputs {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	in := Inputs{Configuration: Configuration{Timezone: "UTC", ReportingYearMonth: 1, ReportingYearDay: 1, SharedAllocationRef: "reviewed shares"}}
	for _, id := range []string{"a", "b", "c"} {
		in.Orders = append(in.Orders, Order{ID: id, Name: id, Start: at, SourceRef: "signed order"})
		in.Meters = append(in.Meters, MeterInterval{ID: id, MeterID: "meter", SourceID: "source", SiteID: 1, ContainerID: 1, OrderID: id, Start: at, End: at.Add(time.Hour), KWh: "100", Quality: "measured", SourceRef: "reading", AllocationFraction: "0.3"})
	}
	return in
}

func TestMeterValidationPreservesEvidenceBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(*Inputs)
		valid bool
	}{
		{"shared", func(*Inputs) {}, true},
		{"adjacent", func(in *Inputs) {
			in.Meters[2].Start = in.Meters[1].End
			in.Meters[2].End = in.Meters[2].Start.Add(time.Hour)
		}, true},
		{"independent_meter", func(in *Inputs) { in.Meters[2].MeterID = "other"; in.Meters[2].AllocationFraction = "" }, true},
		{"independent_source_reference", func(in *Inputs) { in.Meters[2].SourceRef = "another reference" }, true},
		{"duplicate_order", func(in *Inputs) { in.Meters[2].OrderID = in.Meters[0].OrderID }, false},
		{"partial_overlap", func(in *Inputs) { in.Meters[2].Start = in.Meters[2].Start.Add(time.Minute) }, false},
		{"different_end", func(in *Inputs) { in.Meters[2].End = in.Meters[2].End.Add(time.Minute) }, false},
		{"different_energy_spelling", func(in *Inputs) { in.Meters[2].KWh = "100.0" }, false},
		{"different_source", func(in *Inputs) { in.Meters[2].SourceID = "other" }, false},
		{"different_quality", func(in *Inputs) { in.Meters[2].Quality = "estimated" }, false},
		{"different_site", func(in *Inputs) { in.Meters[2].SiteID = 2 }, false},
		{"different_container", func(in *Inputs) { in.Meters[2].ContainerID = 2 }, false},
		{"missing_share", func(in *Inputs) { in.Meters[2].AllocationFraction = "" }, false},
		{"sum_above_one", func(in *Inputs) { in.Meters[2].AllocationFraction = "0.5" }, false},
		{"sum_exactly_one", func(in *Inputs) { in.Meters[2].AllocationFraction = "0.4" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := meterValidationInputs()
			tc.edit(&in)
			for range 2 {
				err := in.Validate()
				if tc.valid {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
				slices.Reverse(in.Meters)
			}
		})
	}
}

func scalableMeterInputs(count int, shared bool) Inputs {
	in := meterValidationInputs()
	base := in.Meters[0]
	in.Meters, in.Orders = nil, nil
	for i := range count {
		id := fmt.Sprint(i)
		in.Orders = append(in.Orders, Order{ID: id, Name: id, Start: base.Start, SourceRef: "order"})
		m := base
		m.ID, m.OrderID = id, id
		if shared {
			m.AllocationFraction = "0.0001"
		} else {
			m.Start = base.Start.Add(time.Duration(i) * time.Hour)
			m.End = m.Start.Add(time.Hour)
			m.AllocationFraction = ""
		}
		in.Meters = append(in.Meters, m)
	}
	return in
}

func TestMeterValidationAtBatchLimit(t *testing.T) {
	for _, shared := range []bool{true, false} {
		in := scalableMeterInputs(5000, shared)
		slices.Reverse(in.Meters)
		require.NoError(t, in.ValidateContext(t.Context()))
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		require.ErrorIs(t, in.ValidateContext(ctx), context.Canceled)
	}
}

func BenchmarkMeterValidation(b *testing.B) {
	for _, shared := range []bool{true, false} {
		for _, count := range []int{100, 1000, 5000} {
			b.Run(fmt.Sprintf("shared=%t/facts=%d", shared, count), func(b *testing.B) {
				in := scalableMeterInputs(count, shared)
				b.ResetTimer()
				for b.Loop() {
					if err := in.Validate(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func TestReportingYearRejectsLeapDayUntilNonLeapRuleIsDefined(t *testing.T) {
	in := Inputs{Configuration: Configuration{Timezone: "UTC", ReportingYearMonth: 2, ReportingYearDay: 29}}
	require.ErrorContains(t, in.Validate(), "February 29")
	in.Configuration.ReportingYearDay = 28
	require.NoError(t, in.Validate())
}

func TestReportingInputAssetKindsPreserveContractualRepairUnits(t *testing.T) {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, kind := range []int32{5, 6, 7} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			in := Inputs{Configuration: Configuration{Timezone: "UTC", ReportingYearMonth: 1, ReportingYearDay: 1},
				Orders:           []Order{{ID: "order", Name: "Order", Start: at, SourceRef: "signed order"}},
				Coverage:         []CoverageSegment{{ID: "coverage", AllocationIntervalID: 1, Kind: kind, AssetID: 1, SiteID: 1, ContainerID: 1, OrderID: "order", Start: at, CapacityTHS: "100", SourceRef: "commissioning"}},
				Hashrates:        []HashrateInterval{{ID: "hash", Kind: kind, AssetID: 1, SiteID: 1, ContainerID: 1, OrderID: "order", Start: at, End: at.Add(time.Hour), HashrateHS: "100", SourceID: "source", SourceRef: "reviewed hash"}},
				PoolObservations: []PoolObservation{{ID: "pool", Kind: kind, AssetID: 1, SourceAt: at, ReceivedAt: at, State: "unknown", SourceID: "pool", SourceRef: "reviewed pool"}},
			}
			require.NoError(t, in.Validate())
			if kind == 7 {
				in.Obligations = []ObligationInterval{{ID: "repair", EpisodeID: "episode", Kind: 7, AssetID: 1, SiteID: 1, ContainerID: 1, OrderID: "order", Start: at, AsOf: at.Add(time.Hour), ReceivedAt: at, SourceRef: "ticket"}}
				require.ErrorContains(t, in.Validate(), "module unit", "do not silently reinterpret module repair obligations as board counts")
			}
		})
	}
}

func TestAccumulatedReportingOrdersHaveNoLifetimeCountQuota(t *testing.T) {
	in := Inputs{Configuration: Configuration{Timezone: "UTC", ReportingYearMonth: 1, ReportingYearDay: 1}}
	for i := range 10001 {
		in.Orders = append(in.Orders, Order{ID: fmt.Sprint(i), Name: "Order", Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), SourceRef: "order"})
	}
	require.NoError(t, in.Validate())
}
