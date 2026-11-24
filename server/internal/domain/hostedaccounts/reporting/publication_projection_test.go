package reporting

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"testing"
	"time"
)

func TestPublicationEffectiveCertificationUsesOriginalGate(t *testing.T) {
	for _, state := range []string{"ready", "unready", "absent"} {
		t.Run(state, func(t *testing.T) {
			p, in := calculationFixture()
			in.Meters = []MeterInterval{{ID: "reading", MeterID: "meter", SourceID: "meter", SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start, End: p.End, KWh: "10", Quality: "measured", SourceRef: "reading"}}
			switch state {
			case "unready":
				in.Configuration.Prerequisites[1].Ready = false
			case "absent":
				in.Configuration.Prerequisites = slices.Delete(in.Configuration.Prerequisites, 1, 2)
			}
			// A different identity with a colliding reference cannot inherit
			// the consulted meter's qualification.
			in.Configuration.Sources = append(in.Configuration.Sources, Source{ID: "unused", Metric: "hashrate", Certified: true, SourceRef: "meter-cert"})
			result := calculate(t, p, in)
			_, _, id, _ := exportFixture()
			members := scopeEvidenceZIP(t, p, in, result, id)
			rows, columns := scopeEvidenceRows(t, members, "source_qualifications")
			if len(rows) != 1 || rows["meter"][slices.Index(columns, "metric")] != "meter" {
				t.Fatalf("qualification identity mismatch: %v", rows)
			}
			row := rows["meter"]
			if row[slices.Index(columns, "certified")] != strconv.FormatBool(state == "ready") {
				t.Fatalf("effective certification does not match %s: %v", state, row)
			}
			if got := row[slices.Index(columns, "source_ref")]; (got == "meter-cert") != (state == "ready") {
				t.Fatalf("unconsumed qualification reference: %v", row)
			}
			if state == "ready" && !bytes.Contains(members["inputs/prerequisites.csv"], []byte("approval:G2")) {
				t.Fatal("effective certification must include supporting G2")
			}
		})
	}
}

func TestPublicationQualificationsExcludeDiscardedPriorPoolCalculation(t *testing.T) {
	p, in := calculationFixture()
	p.Kind, p.End = "annual", p.Start.AddDate(1, 0, 0)
	p.AsOf = p.End
	prior := p.Start.AddDate(-1, 0, 0)
	in.Coverage[0].Start = prior
	in.Configuration.Sources = []Source{
		{ID: "prior-pool", Metric: "pool", Certified: true, SourceRef: "unconsumed prior qualification"},
		{ID: "current-pool", Metric: "pool", Certified: true, SourceRef: "current qualification"},
	}
	in.PoolObservations = []PoolObservation{
		{ID: "previous", Kind: 6, AssetID: 1, SourceID: "prior-pool", SourceAt: prior.Add(time.Hour), ReceivedAt: p.AsOf, State: "reporting", SourceRef: "prior state"},
		{ID: "current", Kind: 6, AssetID: 1, SourceID: "current-pool", SourceAt: p.End.Add(-time.Hour), ReceivedAt: p.AsOf, State: "reporting", SourceRef: "current state"},
	}
	result := calculate(t, p, in)
	// Pass full facts to the configuration projection: its dependency policy
	// must exclude discarded prior pool work independently of row selection.
	config, err := publicEvidenceConfiguration(t.Context(), result, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Sources) != 1 || config.Sources[0].ID != "current-pool" {
		t.Fatalf("discarded prior calculation disclosed qualifications: %+v", config.Sources)
	}
}

func TestPublicationControlIdentityPolicyIsSharedAndDoesNotMutateHost(t *testing.T) {
	r, in, id, observations := exportFixture()
	ref := "curtailment-confirmation:PRIVATE-CONTROL-UUID"
	r.Interruptions = append(r.Interruptions, Interruption{ID: ref + ":PRIVATE-INTERVAL", Start: r.Period.Start, ObservedThrough: r.Period.End, Category: "curtailment_control", Summary: "Current operational explanation", RootCause: "Operational observation; no contractual inference", SourceRef: ref})
	secondWindow := r.Interruptions[len(r.Interruptions)-1]
	secondWindow.ID += "-second"
	r.Interruptions = append(r.Interruptions, secondWindow)
	r.Sources = append(r.Sources, ref)
	original, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	public := PublicResult(r)
	if public.Interruptions[1].ID == public.Interruptions[2].ID || public.Interruptions[1].SourceRef != public.Interruptions[2].SourceRef {
		t.Fatal("control windows need distinct identities and a shared public source")
	}
	encoded, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("PRIVATE-")) || !bytes.Contains(encoded, []byte("operational-observation:1")) {
		t.Fatalf("unsafe detail projection: %s", encoded)
	}
	again, err := json.Marshal(PublicResult(public))
	if err != nil || !bytes.Equal(encoded, again) {
		t.Fatal("public projection is not idempotent")
	}
	files := buildExport(t, r, in, id, observations)
	slices.Reverse(r.Interruptions)
	reordered := buildExport(t, r, in, id, observations)
	slices.Reverse(r.Interruptions)
	if !bytes.Equal(files["evidence.zip"].Data, reordered["evidence.zip"].Data) || !bytes.Equal(files["report.html"].Data, reordered["report.html"].Data) {
		t.Fatal("control input ordering changed public bytes")
	}
	members := unzipExport(t, files["evidence.zip"].Data)
	members["standalone.html"] = files["report.html"].Data
	for name, data := range members {
		if bytes.Contains(data, []byte("PRIVATE-")) {
			t.Errorf("private control identity in %s", name)
		}
	}
	for _, name := range []string{"interruptions.csv", "standalone.html", "manifest.json"} {
		if !bytes.Contains(members[name], []byte("operational-observation:1")) {
			t.Errorf("public control reference missing from %s", name)
		}
	}
	after, err := json.Marshal(r)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("publication modified the host's frozen result")
	}
}
