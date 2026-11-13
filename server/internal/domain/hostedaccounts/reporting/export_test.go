package reporting

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	stdhtml "html"
	"io"
	"math"
	"mime"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEvidenceBudgetsBoundCompressedAndUncompressedOutput(t *testing.T) {
	r, in, id, obs := exportFixture()
	for _, kind := range []string{"archive", "content"} {
		t.Run(kind, func(t *testing.T) {
			archiveBytes, contentBytes := int64(MaxEvidenceZIPBytes), int64(MaxEvidenceContentBytes)
			if kind == "archive" {
				archiveBytes = 100
			} else {
				contentBytes = 32 << 10
			}
			var out bytes.Buffer
			yielded := 0
			err := writeEvidenceContext(t.Context(), &out, r, in, id, ObservationSource{Hours: func(yield func(Observation) error) error {
				for i := int64(1); i <= 25000; i++ {
					v := obs[0]
					v.Bucket = v.Bucket.Add(time.Duration(i) * time.Hour)
					yielded++
					if err := yield(v); err != nil {
						return err
					}
				}
				return nil
			}}, archiveBytes, contentBytes)
			if !errors.Is(err, ErrOutputLimit) {
				t.Fatalf("expected output budget failure, got %v", err)
			}
			if kind == "archive" && int64(out.Len()) > archiveBytes {
				t.Fatal("archive exceeded its byte budget")
			}
			if kind == "content" && yielded >= 25000 {
				t.Fatal("compressible observations bypassed the content budget")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := PreflightArtifacts(ctx, r, in, id); !errors.Is(err, context.Canceled) {
		t.Fatalf("preflight lost cancellation: %v", err)
	}
}

func exportFixture() (Result, Inputs, Identity, []Observation) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := Period{Kind: "monthly", Start: start, End: start.AddDate(0, 1, 0), AsOf: start.AddDate(0, 1, 1), Timezone: "UTC"}
	in := Inputs{Configuration: Configuration{ContractRef: "contract-reviewed", Timezone: "UTC", ReportingYearMonth: 1, ReportingYearDay: 1,
		Prerequisites:   []Prerequisite{{Key: "G1", Owner: "PRIVATE-OWNER", Ready: false, SourceRef: "review-G1"}},
		Representatives: []Representative{{UserID: "PRIVATE-REPRESENTATIVE", SourceRef: "PRIVATE-REP-REF"}}, DeliveryChannel: "PRIVATE-DELIVERY"},
		Orders:           []Order{{ID: "order", Name: "Customer order", Start: start, SourceRef: "order-review"}},
		Coverage:         []CoverageSegment{{ID: "coverage", AllocationIntervalID: 12, Kind: 6, AssetID: 100, SiteID: 3, ContainerID: 4, OrderID: "order", Start: start, CapacityTHS: "100", SourceRef: "coverage-review"}},
		Fees:             []FeeLine{{ID: "fee", SiteID: 3, ContainerID: 4, OrderID: "order", Start: start, End: p.End, Amount: "-12.50", Currency: "USD", SourceRef: "fee-review"}},
		PoolObservations: []PoolObservation{{ID: "pool", AssetID: 100, Kind: 6, SourceID: "pool-source", SourceAt: start, ReceivedAt: start, State: "unavailable", Reason: "PRIVATE-INFRASTRUCTURE-ERROR", SourceRef: "pool-review"}},
		Obligations:      []ObligationInterval{{ID: "obligation", EpisodeID: "episode", AssetID: 100, SiteID: 3, ContainerID: 4, OrderID: "order", Start: start.Add(-24 * time.Hour), AsOf: p.AsOf, TicketRef: "PRIVATE-TICKET", ReceivedAt: start, SourceRef: "obligation-review"}},
	}
	r := Result{Version: "hosted-report-calculation/v1", Period: p, Metrics: map[string]Metric{
		"monthly_uptime":       {Status: Unavailable, Unit: "percent", Causes: []string{"missing_G1"}},
		"metered_energy_kwh":   {Status: Provisional, Value: "12.25", Exact: "49/4", Unit: "kWh", Causes: []string{"uncertified_meter_source"}, Sources: []string{"meter-review"}},
		"accrued_hosting_fees": {Status: Authoritative, Unit: "currency", Amounts: []Amount{{Currency: "USD", Value: "-12.50", Exact: "-25/2"}}},
	}, Containers: []Breakdown{{SiteID: 3, ContainerID: 4, Metrics: map[string]Metric{"metered_energy_kwh": {Status: Provisional, Value: "12.25", Exact: "49/4", Unit: "kWh"}}}},
		Orders:        []Breakdown{{OrderID: "order", Metrics: map[string]Metric{"metered_energy_kwh": {Status: Provisional, Value: "12.25", Exact: "49/4", Unit: "kWh"}}}},
		Interruptions: []Interruption{{ID: "interruption", SiteID: 3, ContainerID: 4, OrderID: "order", Start: start.Add(-time.Hour), ObservedThrough: p.End, Open: true, Category: "outage", Summary: "Reviewed interruption", RootCause: "Reviewed cause", SourceRef: "incident-review"}},
		Obligations:   []ObligationDetail{{ID: "obligation", EpisodeID: "episode", AssetID: 100, SiteID: 3, ContainerID: 4, OrderID: "order", EpisodeStart: start.Add(-24 * time.Hour), IntervalStart: start.Add(-24 * time.Hour), ObservedThrough: p.End, TicketRef: "PRIVATE-TICKET", ReceivedAt: start, SourceRef: "obligation-review"}},
	}
	id := Identity{ReportID: "report", Version: 2, AccountID: "account", AccountName: "Client", IssuedAt: p.AsOf, CorrectionOf: "original", CorrectionReason: "Reviewed late evidence", Exception: "Incomplete source certification"}
	value, complete := -1.5, false
	return r, in, id, []Observation{{IntervalID: 12, AssetID: 100, SiteID: 3, Kind: 6, Bucket: start, Revision: 2, CapturedAt: start.Add(2 * time.Hour), CaptureReason: "closed",
		WindowStart: start, WindowEnd: start.Add(time.Hour), ObservationValues: ObservationValues{PowerReports: 3, PowerCount: 2, PowerSum: &value, PowerComplete: &complete, SourceOldestAt: start, SourceNewestAt: start.Add(time.Minute)}}}
}

func buildExport(t *testing.T, r Result, in Inputs, id Identity, obs []Observation) map[string]ArtifactFile {
	t.Helper()
	files, err := BuildArtifacts(r, in, id, obs)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]ArtifactFile{}
	for _, f := range files {
		out[f.Name] = f
		sum := sha256.Sum256(f.Data)
		if f.SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatalf("incorrect artifact digest: %s", f.Name)
		}
	}
	if len(out) != 2 || out["report.html"].MediaType != "text/html; charset=utf-8" || out["evidence.zip"].MediaType != PublicEvidenceMediaType {
		t.Fatalf("unexpected artifacts: %v", out)
	}
	_, params, err := mime.ParseMediaType(PublicEvidenceMediaType)
	require.NoError(t, err)
	var manifest struct {
		SchemaVersion string `json:"schemaVersion"`
	}
	require.NoError(t, json.Unmarshal(unzipExport(t, out["evidence.zip"].Data)["manifest.json"], &manifest))
	require.Equal(t, manifest.SchemaVersion, params["profile"], "the stored media type names the manifest's schema version")
	return out
}
func unzipExport(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, f := range z.File {
		if _, ok := out[f.Name]; ok {
			t.Fatal("duplicate member")
		}
		h, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		data, e := io.ReadAll(h)
		h.Close()
		if e != nil {
			t.Fatal(e)
		}
		out[f.Name] = data
	}
	return out
}
func TestBuildArtifactsHTMLAndPublicProjection(t *testing.T) {
	r, in, id, obs := exportFixture()
	id.AccountName = `<script>alert("name")</script>`
	r.Interruptions[0].Summary = `<img src=x onerror="alert(1)">`
	r.Reconciliations = []Reconciliation{{Metric: "metered_energy_kwh", View: "container", ExactTotal: "49/4", ExactParts: "49/4", Matches: true, Status: Provisional}}
	r.Deadlines.Contract = Target{Status: Unavailable, Causes: []string{"missing_G5"}, Basis: "approved_business_calendar"}
	before, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	resultBefore, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	observationsBefore, err := json.Marshal(obs)
	if err != nil {
		t.Fatal(err)
	}
	files := buildExport(t, r, in, id, obs)
	html := string(files["report.html"].Data)
	for _, bad := range []string{"<script", "<img", "<iframe", "<link", "metered_energy_kwh", "missing_G5"} {
		if strings.Contains(html, bad) {
			t.Fatalf("active element %q: %s", bad, html)
		}
	}
	for _, want := range []string{"Content-Security-Policy", "default-src &#39;none&#39;", "&lt;script&gt;", "&lt;img", "Monthly uptime", "Unavailable", "Provisional", "Missing G1", "Container breakdown", "Order breakdown", "True episode start", "Period-clipped start", "original", "Incomplete source certification"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q", want)
		}
	}
	styleStart := strings.Index(html, "<style>") + len("<style>")
	styleEnd := strings.Index(html, "</style>")
	if styleStart < len("<style>") || styleEnd < styleStart {
		t.Fatal("missing print stylesheet")
	}
	styleDigest := sha256.Sum256([]byte(html[styleStart:styleEnd]))
	if !strings.Contains(stdhtml.UnescapeString(html), "style-src 'sha256-"+base64.StdEncoding.EncodeToString(styleDigest[:])+"'") {
		t.Fatal("CSP does not authorize exactly the static print stylesheet")
	}
	if !strings.Contains(html, "@media print") || strings.Contains(html, "unsafe-inline") {
		t.Fatal("missing restrictive print support")
	}
	all := unzipExport(t, files["evidence.zip"].Data)
	for name, b := range all {
		for _, bad := range []string{"PRIVATE-", `"representatives"`, `"ticketRef"`, `"deliveryChannel"`, "device_identifier", "origin_key"} {
			if bytes.Contains(b, []byte(bad)) {
				t.Fatalf("private data %q in %s", bad, name)
			}
		}
	}
	after, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("export changed source inputs")
	}
	resultAfter, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	observationsAfter, err := json.Marshal(obs)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(resultBefore, resultAfter) || !bytes.Equal(observationsBefore, observationsAfter) {
		t.Fatal("export changed source result or observations")
	}
}
func TestBuildArtifactsCSVTextAndNumericSafety(t *testing.T) {
	r, in, id, obs := exportFixture()
	in.Orders = nil
	for i, name := range []string{"=SUM(A1:A2)", "+cmd", "-text", "@query", "\t \r=cmd", "quotes, \"hello\"\n世界"} {
		in.Orders = append(in.Orders, Order{ID: string(rune('a' + i)), Name: name, SourceRef: "review"})
	}
	files := buildExport(t, r, in, id, obs)
	members := unzipExport(t, files["evidence.zip"].Data)
	rows, err := csv.NewReader(bytes.NewReader(members["inputs/orders.csv"])).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	nameColumn := slices.Index(rows[0], "name")
	if nameColumn < 0 {
		t.Fatal("missing name column")
	}
	for i, row := range rows[1:] {
		want := in.Orders[i].Name
		if i < 5 {
			want = "'" + want
		}
		if row[nameColumn] != want {
			t.Fatalf("cell %q want %q", row[nameColumn], want)
		}
	}
	feeRows, err := csv.NewReader(bytes.NewReader(members["inputs/fees.csv"])).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if feeRows[1][slices.Index(feeRows[0], "amount")] != "-12.50" {
		t.Fatal("negative numeric amount was changed")
	}
	obsRows, err := csv.NewReader(bytes.NewReader(members["observations.csv"])).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if obsRows[1][slices.Index(obsRows[0], "power_sum_w")] != "-1.5" || obsRows[1][slices.Index(obsRows[0], "power_complete")] != "false" || obsRows[1][slices.Index(obsRows[0], "hashrate_sum_hs")] != "" {
		t.Fatal("negative observation changed")
	}
}
func TestBuildArtifactsDeterministicDigestsAndFrozenBytes(t *testing.T) {
	r, in, id, obs := exportFixture()
	in.Orders = append(in.Orders, Order{ID: "other", Name: "Other", SourceRef: "other-review"})
	secondFee := in.Fees[0]
	secondFee.ID = "second-fee"
	secondFee.Amount = "2.75"
	in.Fees = append(in.Fees, secondFee)
	in.Configuration.Prerequisites = append(in.Configuration.Prerequisites, Prerequisite{Key: "G2", Owner: "PRIVATE-SECOND-OWNER", SourceRef: "review-G2"})
	secondInterruption := r.Interruptions[0]
	secondInterruption.ID = "second-interruption"
	r.Interruptions = append(r.Interruptions, secondInterruption)
	secondContainer := r.Containers[0]
	secondContainer.SiteID = 5
	secondContainer.ContainerID = 6
	r.Containers = append(r.Containers, secondContainer)
	secondOrder := r.Orders[0]
	secondOrder.OrderID = "other"
	r.Orders = append(r.Orders, secondOrder)
	obs = append(obs, Observation{IntervalID: 11, AssetID: 101, SiteID: 3, Kind: 7, Bucket: r.Period.Start, CapturedAt: r.Period.End, CaptureReason: "gap", GapReason: "closer fell behind past the correction window"})
	r.Metrics["metered_energy_kwh"] = Metric{Status: Provisional, Value: "12.25", Exact: "49/4", Unit: "kWh", Causes: []string{"z", "a"}, Sources: []string{"z-ref", "a-ref"}}
	first := buildExport(t, r, in, id, obs)
	slices.Reverse(in.Orders)
	slices.Reverse(in.Fees)
	slices.Reverse(in.Configuration.Prerequisites)
	slices.Reverse(r.Containers)
	slices.Reverse(r.Orders)
	slices.Reverse(r.Interruptions)
	slices.Reverse(obs)
	m := r.Metrics["metered_energy_kwh"]
	slices.Reverse(m.Causes)
	slices.Reverse(m.Sources)
	r.Metrics["metered_energy_kwh"] = m
	second := buildExport(t, r, in, id, obs)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("input ordering changed artifacts")
	}
	members := unzipExport(t, first["evidence.zip"].Data)
	for name := range members {
		if name != "manifest.json" && !strings.HasSuffix(name, ".csv") {
			t.Fatalf("evidence bundle contains a non-CSV member: %s", name)
		}
	}
	var manifest struct {
		SchemaVersion string `json:"schemaVersion"`
		Files         []struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
		Limitations []string `json:"limitations"`
	}
	if err := json.Unmarshal(members["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion == "" || len(manifest.Files) != len(members)-1 {
		t.Fatal("manifest must cover every member except itself")
	}
	if _, ok := members["observations.csv"]; !ok {
		t.Fatal("hourly evidence member missing")
	}
	if !slices.ContainsFunc(manifest.Limitations, func(l string) bool {
		return strings.Contains(l, "rollups in observation_days.csv instead of observations.csv.")
	}) {
		t.Fatalf("manifest must name the real observation members: %q", manifest.Limitations)
	}
	for _, f := range manifest.Files {
		sum := sha256.Sum256(members[f.Name])
		if hex.EncodeToString(sum[:]) != f.SHA256 {
			t.Fatalf("bad member digest %s", f.Name)
		}
	}
	frozen := bytes.Clone(first["evidence.zip"].Data)
	in.Orders[0].Name = "new source revision"
	obs[0].CapturedAt = obs[0].CapturedAt.Add(time.Hour)
	delete(r.Metrics, "monthly_uptime")
	_ = buildExport(t, r, in, id, obs)
	if !bytes.Equal(first["evidence.zip"].Data, frozen) {
		t.Fatal("source revision modified already-built artifact")
	}
}
func TestBuildArtifactsRejectsCredentialReferencesAndNonFiniteObservations(t *testing.T) {
	for _, ref := range []string{"https://name:password@example.test/evidence", "https://example.test/evidence?token=secret", "https://example.test/evidence?%74oken=secret", "https://example.test/evidence?X-Amz-Signature=secret", "password=secret"} {
		t.Run(ref, func(t *testing.T) {
			r, in, id, obs := exportFixture()
			in.Coverage[0].SourceRef = ref
			if _, err := BuildArtifacts(r, in, id, obs); err == nil {
				t.Fatal("credential reference accepted")
			}
		})
	}
	r, in, id, obs := exportFixture()
	r.Metrics["monthly_uptime"] = Metric{Status: Unavailable, Value: "123", Exact: "123", Unit: "percent", Causes: []string{"missing_G1"}}
	files := buildExport(t, r, in, id, obs)
	members := unzipExport(t, files["evidence.zip"].Data)
	rows, e := csv.NewReader(bytes.NewReader(members["metrics.csv"])).ReadAll()
	if e != nil {
		t.Fatal(e)
	}
	for _, row := range rows[1:] {
		if row[slices.Index(rows[0], "metric")] == "monthly_uptime" && row[slices.Index(rows[0], "value")] != "" {
			t.Fatal("unavailable numeric value leaked")
		}
	}
}

type exportCountingWriter struct {
	bytes  int64
	failed bool
}

func (w *exportCountingWriter) Write(p []byte) (int, error) {
	if w.failed {
		return 0, errors.New("storage write failed")
	}
	w.bytes += int64(len(p))
	return len(p), nil
}

func TestWriteEvidenceStreamsObservationsAndPropagatesFailures(t *testing.T) {
	r, in, id, obs := exportFixture()
	t.Run("incremental output", func(t *testing.T) {
		var out exportCountingWriter
		err := WriteEvidence(&out, r, in, id, ObservationSource{Hours: func(yield func(Observation) error) error {
			baseline := out.bytes
			for i := int64(1); i <= 25000; i++ {
				v := obs[0]
				v.Bucket = v.Bucket.Add(time.Duration(i) * time.Hour)
				value := float64(i) * 1.123456789
				v.HashrateSum = &value
				if e := yield(v); e != nil {
					return e
				}
				if i == 15000 && out.bytes <= baseline {
					t.Fatal("observations were buffered instead of streamed")
				}
			}
			return nil
		}})
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("source error", func(t *testing.T) {
		sentinel := errors.New("source failed")
		err := WriteEvidence(io.Discard, r, in, id, ObservationSource{Hours: func(yield func(Observation) error) error {
			if e := yield(obs[0]); e != nil {
				return e
			}
			return sentinel
		}})
		if !errors.Is(err, sentinel) {
			t.Fatalf("lost source error: %v", err)
		}
	})
	t.Run("storage error", func(t *testing.T) {
		var out exportCountingWriter
		produced := 0
		err := WriteEvidence(&out, r, in, id, ObservationSource{Hours: func(yield func(Observation) error) error {
			out.failed = true
			for i := int64(1); i <= 25000; i++ {
				produced++
				v := obs[0]
				v.Bucket = v.Bucket.Add(time.Duration(i) * time.Hour)
				value := float64(i) * 1.123456789
				v.HashrateSum = &value
				if e := yield(v); e != nil {
					return e
				}
			}
			return nil
		}})
		if err == nil || produced >= 25000 {
			t.Fatalf("storage failure did not abort incremental stream: produced=%d err=%v", produced, err)
		}
	})
	t.Run("out of order", func(t *testing.T) {
		err := WriteEvidence(io.Discard, r, in, id, ObservationSource{Hours: func(yield func(Observation) error) error {
			if e := yield(obs[0]); e != nil {
				return e
			}
			return yield(obs[0])
		}})
		if err == nil {
			t.Fatal("repeated hour accepted")
		}
	})
	t.Run("ignored yield error", func(t *testing.T) {
		err := WriteEvidence(io.Discard, r, in, id, ObservationSource{Hours: func(yield func(Observation) error) error { _ = yield(obs[0]); _ = yield(obs[0]); return nil }})
		if err == nil {
			t.Fatal("provider swallowed an error and produced a successful archive")
		}
	})
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		t.Run("nonfinite", func(t *testing.T) {
			observation := obs[0]
			observation.TemperatureLatest = &v
			err := WriteEvidence(io.Discard, r, in, id, ObservationSource{Hours: func(yield func(Observation) error) error { return yield(observation) }})
			if err == nil {
				t.Fatal("nonfinite observation accepted")
			}
		})
	}
}

func TestWriteEvidenceMatchesConvenienceArtifacts(t *testing.T) {
	r, in, id, obs := exportFixture()
	files := buildExport(t, r, in, id, obs)
	var stream bytes.Buffer
	if err := WriteEvidence(&stream, r, in, id, ObservationSource{Hours: func(yield func(Observation) error) error { return yield(obs[0]) }}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stream.Bytes(), files["evidence.zip"].Data) {
		t.Fatal("streaming and in-memory archives differ")
	}
	html, err := BuildWrittenReport(r, id)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(html, files["report.html"].Data) {
		t.Fatal("written artifact helper differs")
	}
	members := unzipExport(t, stream.Bytes())
	rows, err := csv.NewReader(bytes.NewReader(members["obligations.csv"])).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"true_episode_start": "2025-12-31T00:00:00Z", "period_clipped_start": "2026-01-01T00:00:00Z", "period_clipped_end": "2026-02-01T00:00:00Z"} {
		if got := rows[1][slices.Index(rows[0], key)]; got != want {
			t.Fatalf("%s = %s want %s", key, got, want)
		}
	}
}

func TestPublicEvidenceMetersContainOnlySupportedAttributedConsumption(t *testing.T) {
	r, in, id, obs := exportFixture()
	in.Configuration.SharedAllocationRef = "reviewed meter allocation"
	in.Configuration.Sources = []Source{{ID: "meter", Metric: "meter", SourceRef: "meter qualification", Certified: false}}
	shared := MeterInterval{ID: "shared", MeterID: "physical-meter", SourceID: "meter", SiteID: 3, ContainerID: 4, OrderID: "order", Start: r.Period.Start, End: r.Period.Start.Add(24 * time.Hour), KWh: "98765.4321000000000000001", Quality: "estimated", AllocationFraction: "0.25", SourceRef: "shared meter source"}
	in.Meters = []MeterInterval{shared}
	direct := shared
	direct.ID = "direct"
	direct.MeterID = "dedicated-meter"
	direct.Start = shared.End
	direct.End = direct.Start.Add(24 * time.Hour)
	direct.KWh = "19.125"
	direct.Quality = "measured"
	direct.AllocationFraction = "1"
	in.Meters = append(in.Meters, direct)
	for _, quality := range []string{"missing", "reset"} {
		next := direct
		next.ID = quality
		next.Quality = quality
		next.KWh = ""
		next.AllocationFraction = ""
		in.Meters = append(in.Meters, next)
	}
	for _, kind := range []string{"outside", "boundary", "unattributed"} {
		next := shared
		next.ID = "omitted-" + kind
		next.SourceRef = "omitted-source-" + kind
		next.KWh = "87654321.987654321"
		switch kind {
		case "outside":
			next.Start = r.Period.End
			next.End = next.Start.Add(time.Hour)
		case "boundary":
			next.Start = r.Period.Start.Add(-time.Hour)
		case "unattributed":
			next.OrderID = "other-client-order"
		}
		in.Meters = append(in.Meters, next)
	}
	before, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	members := unzipExport(t, buildExport(t, r, in, id, obs)["evidence.zip"].Data)
	for name, data := range members {
		for _, private := range []string{shared.KWh, "0.25", "87654321.987654321", "allocation_fraction", "allocationFraction", "omitted-", "omitted-source-", "other-client-order"} {
			if bytes.Contains(data, []byte(private)) {
				t.Fatalf("private or unsupported meter evidence %q in %s", private, name)
			}
		}
	}
	meters, err := csv.NewReader(bytes.NewReader(members["inputs/meters.csv"])).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(meters) != 5 {
		t.Fatalf("unexpected meter table %+v", meters)
	}
	amount := slices.Index(meters[0], "attributed_kwh")
	quality := slices.Index(meters[0], "quality")
	key := slices.Index(meters[0], "id")
	if amount < 0 || quality < 0 || key < 0 {
		t.Fatalf("missing meter columns: %v", meters[0])
	}
	values := map[string]string{}
	for _, row := range meters[1:] {
		values[row[key]] = row[amount]
		if row[key] == "shared" && row[quality] != "estimated" {
			t.Fatal("source quality changed")
		}
	}
	if values["shared"] != "24691.358025000000000000025" || values["direct"] != "19.125" || values["missing"] != "" || values["reset"] != "" {
		t.Fatalf("attributed consumption %+v", values)
	}
	if !bytes.Contains(members["inputs/source_qualifications.csv"], []byte("meter qualification")) || !bytes.Contains(members["manifest.json"], []byte("hosted-report-evidence/v3")) {
		t.Fatal("missing source qualification or export policy")
	}
	after, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("public projection modified raw reviewed evidence")
	}
}

func TestEvidenceExportsGapHoursAndDailyRollupsForLongPeriods(t *testing.T) {
	r, in, id, obs := exportFixture()
	gap := Observation{IntervalID: 12, AssetID: 100, SiteID: 3, Kind: 6, Bucket: obs[0].Bucket.Add(time.Hour), CapturedAt: obs[0].CapturedAt, CaptureReason: "gap", GapReason: "=closer fell behind"}
	members := unzipExport(t, buildExport(t, r, in, id, []Observation{gap, obs[0]})["evidence.zip"].Data)
	rows, err := csv.NewReader(bytes.NewReader(members["observations.csv"])).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 3)
	column := func(name string) int { return slices.Index(rows[0], name) }
	require.Equal(t, []string{"closed", "2", ""}, []string{rows[1][column("capture_reason")], rows[1][column("revision")], rows[1][column("gap_reason")]})
	require.Equal(t, []string{"gap", "0", "'=closer fell behind", ""}, []string{rows[2][column("capture_reason")], rows[2][column("revision")], rows[2][column("gap_reason")], rows[2][column("power_sum_w")]})
	for _, invalid := range []Observation{{IntervalID: 12, Bucket: gap.Bucket, CapturedAt: gap.CapturedAt, CaptureReason: "gap", Revision: 1, GapReason: "gap"}, {IntervalID: 12, Bucket: gap.Bucket, CapturedAt: gap.CapturedAt, CaptureReason: "closed"}, {IntervalID: 12, Bucket: gap.Bucket, CapturedAt: gap.CapturedAt, CaptureReason: "INSERT", Revision: 1}} {
		_, err := BuildArtifacts(r, in, id, []Observation{invalid})
		require.Error(t, err)
	}

	// Covered intervals times period hours past the limit switch to day rollups.
	require.False(t, DailyObservations(r.Period, in.Coverage))
	covered := in.Coverage[0]
	covered.Start, covered.End = r.Period.Start, time.Time{}
	many := make([]CoverageSegment, 1000)
	for i := range many {
		many[i] = covered
		many[i].AllocationIntervalID = int64(i/2 + 1) // two segments of one interval count once
	}
	span := Period{Start: r.Period.Start, End: r.Period.Start.Add(1000 * time.Hour)}
	require.False(t, DailyObservations(span, many), "500 intervals over 1000 hours is the limit")
	span.End = span.End.Add(time.Microsecond)
	require.True(t, DailyObservations(span, many))
	many[0].Start, many[1].Start = span.End, span.End
	require.False(t, DailyObservations(span, many), "coverage outside the period does not count")
	// A single interval reaches the limit only over a very long period.
	in.Coverage = []CoverageSegment{covered}
	r.Period.End = r.Period.Start.Add((MaxHourlyObservations + 1) * time.Hour)
	r.Period.AsOf = r.Period.End
	require.True(t, DailyObservations(r.Period, in.Coverage))
	day := obs[0].Bucket.Truncate(24 * time.Hour)
	sum := 7.5
	source := ObservationSource{
		Hours: func(func(Observation) error) error { return errors.New("a long period must not read hourly rows") },
		Days: func(yield func(ObservationDay) error) error {
			for _, v := range []ObservationDay{
				{IntervalID: 12, AssetID: 100, SiteID: 3, Kind: 6, Day: day, Hours: 23, GapHours: 1, RevisedHours: 2, NoSourceDataHours: 3, Revision: 4, CapturedAt: day.Add(30 * time.Hour), ObservationValues: ObservationValues{HashrateReports: 80, HashrateCount: 79, HashrateSum: &sum}},
				{IntervalID: 12, AssetID: 100, SiteID: 3, Kind: 6, Day: day.Add(24 * time.Hour), GapHours: 24, CapturedAt: day.Add(60 * time.Hour)},
			} {
				if err := yield(v); err != nil {
					return err
				}
			}
			return nil
		},
	}
	var out bytes.Buffer
	require.NoError(t, WriteEvidence(&out, r, in, id, source))
	members = unzipExport(t, out.Bytes())
	require.NotContains(t, members, "observations.csv")
	rows, err = csv.NewReader(bytes.NewReader(members["observation_days.csv"])).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Equal(t, []string{"23", "1", "2", "3", "4", "79", "7.5"}, []string{rows[1][exportColumn(rows, "hours")], rows[1][exportColumn(rows, "gap_hours")], rows[1][exportColumn(rows, "revised_hours")], rows[1][exportColumn(rows, "no_source_data_hours")], rows[1][exportColumn(rows, "revision")], rows[1][exportColumn(rows, "hashrate_count")], rows[1][exportColumn(rows, "hashrate_sum_hs")]})
	require.Equal(t, []string{"0", "24"}, []string{rows[2][exportColumn(rows, "hours")], rows[2][exportColumn(rows, "gap_hours")]})
	source.Days = func(yield func(ObservationDay) error) error {
		return yield(ObservationDay{IntervalID: 12, Day: day, Hours: 20, GapHours: 5, CapturedAt: day})
	}
	require.Error(t, WriteEvidence(io.Discard, r, in, id, source), "a UTC day has at most 24 hours")
}
func exportColumn(rows [][]string, name string) int { return slices.Index(rows[0], name) }
