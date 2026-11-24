package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestASICHighMatchesDetailAcrossTransports(t *testing.T) {
	for _, count := range []int{0, 1, 3, 312} {
		for _, miningState := range []string{miningStateMining, miningStateIdle} {
			t.Run(fmt.Sprintf("%d/%s", count, miningState), func(t *testing.T) {
				cfg := testConfig()
				cfg.Tanks, cfg.ModulesPerTank, cfg.SlotsPerModule, cfg.AsicsPerBoard = 1, 1, 1, count
				state := newContainerState(cfg, time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC))
				state.now = func() time.Time { return state.StartTime }
				board := state.Modules[0].Slots[0].Board
				board.State = miningState
				mux := http.NewServeMux()
				NewRESTApiHandler(state).RegisterRoutes(mux)
				server := httptest.NewServer(mux)
				t.Cleanup(server.Close)

				var detail asicsResponse
				getJSON(t, server, "/api/v2/hashboards/"+board.MAC+"/asics", &detail)
				var restJSON json.RawMessage
				getJSON(t, server, "/api/v2/container/map", &restJSON)
				var rest mdk.GetContainerMapResponse
				if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(restJSON, &rest); err != nil {
					t.Fatal(err)
				}
				api := grpcAPI{state: state}
				wire, err := api.GetContainerMap(t.Context(), &mdk.GetContainerMapRequest{})
				if err != nil {
					t.Fatal(err)
				}
				health := wire.ContainerMap.Tanks[0].Modules[0].Slots[0].Hashboard.Health
				restHealth := rest.ContainerMap.Tanks[0].Modules[0].Slots[0].Hashboard.Health
				if count == 0 {
					if health.MaximumAsicTemperatureC != nil || restHealth.MaximumAsicTemperatureC != nil || health.AsicTemperatureObservedAt != nil || restHealth.AsicTemperatureObservedAt != nil {
						t.Fatal("no ASICs must not produce a maximum temperature")
					}
					return
				}
				if health.MaximumAsicTemperatureC == nil || restHealth.MaximumAsicTemperatureC == nil {
					t.Fatal("ASIC high is absent from container health")
				}
				maximum := math.Inf(-1)
				for _, asic := range detail.Asics {
					maximum = math.Max(maximum, float64(asic.TemperatureDc)/10)
				}
				closeTo(t, "gRPC ASIC high", *health.MaximumAsicTemperatureC, maximum, 1e-9)
				closeTo(t, "REST ASIC high", *restHealth.MaximumAsicTemperatureC, maximum, 1e-9)
				if health.AsicTemperatureObservedAt == nil || restHealth.AsicTemperatureObservedAt == nil || !health.AsicTemperatureObservedAt.AsTime().Equal(health.Timestamp.AsTime()) || !restHealth.AsicTemperatureObservedAt.AsTime().Equal(health.Timestamp.AsTime()) {
					t.Fatal("ASIC summary must use the shared simulation sample time")
				}
			})
		}
	}
}
