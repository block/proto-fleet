package ct1

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Representative full CT1 map: six tanks, 96 modules, 288 installed boards.
// This measures local normalization, not network time or controller CPU.
func BenchmarkCT1PollNormalization(b *testing.B) {
	now := time.Now().UTC()
	tanks := []any{}
	for tank := 1; tank <= 6; tank++ {
		modules := []any{}
		for module := 1; module <= 16; module++ {
			modules = append(modules, pollingCorrectionsModule(now, module, fmt.Sprintf("t%d-m%d", tank, module), [2]float64{100, 60}, [2]float64{100, 65}, [2]float64{100, 70}))
		}
		tanks = append(tanks, map[string]any{"id": tank, "modules": modules})
	}
	raw, err := json.Marshal(map[string]any{"containerMap": map[string]any{"tanks": tanks}})
	if err != nil {
		b.Fatal(err)
	}
	wire := &mdk.GetContainerMapResponse{}
	if err := protojson.Unmarshal(raw, wire); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		s, err := snapshot(mapResponse(wire), now)
		if err != nil {
			b.Fatal(err)
		}
		sdk.CalculateContainerRollups(s)
	}
	b.ReportMetric(float64(proto.Size(wire)), "wire-bytes")
}
