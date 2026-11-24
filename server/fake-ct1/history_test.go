package main

import (
	"math"
	"testing"
)

func TestTemperatureHistoryUsesBoardSensorsIncludingSleepingBoards(t *testing.T) {
	_, state := newTestServer(t)
	boards := state.eachBoard()
	boards[0].State = miningStateIdle
	sample := state.recordSample()
	var total float64
	for i, board := range boards {
		live := state.boardSample(board)
		if sample.Boards[i].Temperature != live.BoardTemp {
			t.Fatalf("board %d history temperature = %v, want board sensor %v (ASIC %v)", i, sample.Boards[i].Temperature, live.BoardTemp, live.AsicTemp)
		}
		total += live.BoardTemp
	}
	got, ok := sample.metric(timeSeriesMetricTemperature, "", -1)
	if want := total / float64(len(boards)); !ok || math.Abs(got-want) > 1e-9 {
		t.Fatalf("container temperature = %v (available %v), want %v", got, ok, want)
	}
	got, ok = sample.metric(timeSeriesMetricTemperature, "", 0)
	if !ok || got != state.boardSample(boards[0]).BoardTemp {
		t.Fatalf("sleeping board temperature = %v (available %v)", got, ok)
	}
}
