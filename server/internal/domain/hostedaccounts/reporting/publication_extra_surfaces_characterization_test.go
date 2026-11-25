package reporting

import (
	"bytes"
	"reflect"
	"slices"
	"testing"
	"time"
)

// Current explanations, request metadata and frozen observations have their own
// publication purpose even when changing their prose leaves metric values equal.
func TestPublicationRetainsRequiredIndependentSurfaces(t *testing.T) {
	p, in := calculationFixture()
	p.Kind = "ad_hoc"
	p.RequestRef = "CURRENT-REQUEST-REF"
	p.RequestReceivedAt = p.Start
	in.Configuration.Registers = append(in.Configuration.Registers, RegisterCompleteness{Kind: "incidents", Start: p.Start, End: p.End, SourceRef: "incident-register"})
	in.Incidents = []Incident{{ID: "incident", SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start.Add(time.Hour), End: p.Start.Add(2 * time.Hour), AsOf: p.AsOf, Category: "outage", Summary: "CURRENT-INCIDENT-SUMMARY", RootCause: "CURRENT-CAUSE-TEXT", ExclusionClause: "CURRENT-EXCLUSION-TEXT", SourceRef: "incident-ref"}}
	result := calculate(t, p, in)

	// Those narrative fields and the request reference are not inputs to the
	// final metric maps, although root-cause presence affects incident metrics.
	other := in
	other.Incidents = slices.Clone(in.Incidents)
	other.Incidents[0].Summary = "other summary"
	other.Incidents[0].RootCause = "other nonempty cause"
	other.Incidents[0].ExclusionClause = "other claim"
	otherPeriod := p
	otherPeriod.RequestRef = "another request"
	otherResult := calculate(t, otherPeriod, other)
	if !reflect.DeepEqual(result.Metrics, otherResult.Metrics) || !reflect.DeepEqual(result.Containers, otherResult.Containers) || !reflect.DeepEqual(result.Orders, otherResult.Orders) {
		t.Fatal("fixture values unexpectedly changed metric maps")
	}
	// Preparation appends controls separately, after the reviewed calculation.
	result.Interruptions = append(result.Interruptions, Interruption{ID: "control", SiteID: 1, ContainerID: 10, Start: p.Start, End: p.End, ObservedThrough: p.End, Category: "curtailment_control", Summary: "CURRENT-CONTROL-SUMMARY", SourceRef: "CURRENT-CONTROL-REF"})
	result.Sources = append(result.Sources, "CURRENT-CONTROL-REF")
	observation := Observation{IntervalID: 1, AssetID: 1, SiteID: 1, Kind: 6, Bucket: p.Start, Revision: 1, CapturedAt: p.Start, CaptureReason: "closed", WindowStart: p.Start, WindowEnd: p.Start.Add(time.Hour)}
	_, _, identity, _ := exportFixture()
	selected, err := SelectPublicEvidence(t.Context(), p, in, ObservationSource{})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := WriteEvidenceContext(t.Context(), &out, result, selected, identity, ObservationSource{Hours: func(yield func(Observation) error) error { return yield(observation) }}); err != nil {
		t.Fatal(err)
	}
	members := unzipExport(t, out.Bytes())
	written, err := BuildWrittenReport(result, identity)
	if err != nil {
		t.Fatal(err)
	}
	members["standalone.html"] = written
	for member, sentinel := range map[string]string{
		"inputs/incidents.csv": "CURRENT-INCIDENT-SUMMARY",
		"interruptions.csv":    "CURRENT-CONTROL-SUMMARY",
		"standalone.html":      "CURRENT-CONTROL-SUMMARY",
		"manifest.json":        "CURRENT-CONTROL-REF",
		"observations.csv":     "closed",
		"deadlines.csv":        "CURRENT-REQUEST-REF",
	} {
		if !bytes.Contains(members[member], []byte(sentinel)) {
			t.Errorf("required disclosure absent: %s lacks %s", member, sentinel)
		}
	}
}
