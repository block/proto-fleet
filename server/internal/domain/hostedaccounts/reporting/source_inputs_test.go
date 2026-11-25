package reporting

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type sourceStreamSink struct{ total, maxWrite int }

func (s *sourceStreamSink) Write(p []byte) (int, error) {
	s.total += len(p)
	s.maxWrite = max(s.maxWrite, len(p))
	return len(p), nil
}

func TestSourceInputsStreamBoundsBufferingAndStopsOnCancellation(t *testing.T) {
	p, in := calculationFixture()
	payload := json.RawMessage(`{"id":"fact","sourceRef":"` + strings.Repeat("s", 4096) + `"}`)
	sink := &sourceStreamSink{}
	ctx, cancel := context.WithCancel(t.Context())
	err := WriteSourceInputsStream(ctx, sink, p, in.Configuration, func(kind FactKind, yield func(json.RawMessage) error) error {
		if kind != FactFee {
			return nil
		}
		for i := range 4096 {
			if err := yield(payload); err != nil {
				return err
			}
			if i == 4095 {
				cancel()
			}
		}
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Greater(t, sink.total, 16<<20)
	require.LessOrEqual(t, sink.maxWrite, len(payload), "writer never encodes a register or complete artifact")
}

func TestSourceInputsStreamEmitsEmptyArraysAndPreservesOriginalJSON(t *testing.T) {
	p, in := calculationFixture()
	var file bytes.Buffer
	original := json.RawMessage(`{"id":"original","amount":"1.00000000000000000001","start":"2026-09-01T00:00:00.000000123Z"}`)
	err := WriteSourceInputsStream(t.Context(), &file, p, in.Configuration, func(kind FactKind, yield func(json.RawMessage) error) error {
		if kind == FactFee {
			return yield(original)
		}
		return nil
	})
	require.NoError(t, err)
	var envelope struct {
		Inputs map[string]json.RawMessage `json:"inputs"`
	}
	require.NoError(t, json.Unmarshal(file.Bytes(), &envelope))
	for _, name := range []string{"orders", "coverage", "moduleCoverage", "meters", "incidents", "hashrates", "poolObservations", "obligations", "externalResults"} {
		require.JSONEq(t, "[]", string(envelope.Inputs[name]))
	}
	require.JSONEq(t, "["+string(original)+"]", string(envelope.Inputs["fees"]))
}
