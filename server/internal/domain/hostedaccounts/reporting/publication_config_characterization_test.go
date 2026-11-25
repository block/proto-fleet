package reporting

import (
	"slices"
	"testing"
)

// Publication qualifications follow consulted identities, never coincident references.
func TestPublicationConfigOmitsUnconsultedReferenceCollision(t *testing.T) {
	p, in := calculationFixture()
	in.Coverage[0].SourceRef = "shared-reference"
	in.Configuration.Sources = []Source{
		{ID: "overlap-a", Metric: "hashrate", Certified: true, SourceRef: "shared-reference"},
		{ID: "overlap-b", Metric: "hashrate", Certified: true, SourceRef: "other-qualification"},
	}
	h := HashrateInterval{ID: "a", Kind: 6, AssetID: 1, SiteID: 1, ContainerID: 10, OrderID: "a", SourceID: "overlap-a", Start: p.Start, End: p.End, HashrateHS: "42", Complete: true, SourceRef: "hash-a"}
	other := h
	other.ID, other.SourceID, other.SourceRef = "b", "overlap-b", "hash-b"
	in.Hashrates = []HashrateInterval{h, other}
	result, err := CalculateContext(t.Context(), p, in)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(result.Metrics["hashrate_performance_hs"].Causes, "overlapping_hashrate_evidence") || !slices.Contains(result.Sources, "shared-reference") {
		t.Fatal("fixture must contain a hashrate conflict and the coverage reference")
	}
	_, _, identity, _ := exportFixture()
	members := scopeEvidenceZIP(t, p, in, result, identity)
	rows, columns := scopeEvidenceRows(t, members, "source_qualifications")
	if _, ok := rows["overlap-a"]; ok {
		t.Fatalf("unconsulted qualification leaked: %v, columns %v", rows, columns)
	}
	prerequisites, columns := scopeEvidenceTable(t, members, "prerequisites")
	key := slices.Index(columns, "key")
	if key < 0 {
		t.Fatal("missing prerequisite key column")
	}
	for _, row := range prerequisites {
		if row[key] == "G2" {
			t.Fatal("unconsulted G2 must not be disclosed")
		}
	}
}

func TestPublicationConfigIncludesConsultedReadyG1(t *testing.T) {
	p, in := calculationFixture()
	p.Kind = "ad_hoc"
	in.Orders, in.Coverage = nil, nil
	result, err := CalculateContext(t.Context(), p, in)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(result.Sources, "approval:G1") || slices.Contains(result.Metrics["monthly_uptime"].Causes, "missing_G1") {
		t.Fatal("fixture must not carry the G1 reference or missing-G1 cause")
	}
	_, _, identity, _ := exportFixture()
	members := scopeEvidenceZIP(t, p, in, result, identity)
	rows, columns := scopeEvidenceTable(t, members, "prerequisites")
	key := slices.Index(columns, "key")
	if key < 0 {
		t.Fatal("missing prerequisite key column")
	}
	for _, row := range rows {
		if row[key] == "G1" {
			return
		}
	}
	t.Fatal("consulted ready G1 is missing")
}
