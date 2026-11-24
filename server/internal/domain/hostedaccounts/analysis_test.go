package hostedaccounts

import (
	"context"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAnalysisCSVPreservesNegativeNumbersAndNeutralizesFormulas(t *testing.T) {
	at := timestamppb.New(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	result := &pb.AccountAnalysis{Name: "=-1+2", Start: at, End: at, CreatedAt: at, Tables: []*pb.AnalysisTable{{Rows: []*pb.AnalysisRow{{Cells: []string{"-1", "-0.000000000000000001", "-1+2", " +SUM(A1)", "@cmd", "\t=1"}}}}}}
	data, err := analysisCSV(result)
	require.NoError(t, err)
	r := csv.NewReader(strings.NewReader(string(data)))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	require.NoError(t, err)
	require.Equal(t, []string{"Analysis", "'=-1+2"}, rows[0])
	require.Equal(t, []string{"-1", "-0.000000000000000001", "'-1+2", "' +SUM(A1)", "'@cmd", "'\t=1"}, rows[len(rows)-1])
}

func TestAnalysisGenerationAdmissionIsBoundedAndCancelable(t *testing.T) {
	var releases []func()
	for range analysisGenerationCapacity {
		release, err := acquireAnalysisGeneration(t.Context())
		require.NoError(t, err)
		releases = append(releases, release)
	}
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	ctx, cancel := context.WithCancel(t.Context())
	started, done := make(chan struct{}), make(chan error, 1)
	go func() { close(started); _, err := acquireAnalysisGeneration(ctx); done <- err }()
	<-started
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	_, err := acquireAnalysisGeneration(t.Context())
	require.True(t, fleeterror.IsResourceExhaustedError(err), "full global capacity has bounded wait")
	releases[0]()
	releases = releases[1:]
	release, err := acquireAnalysisGeneration(t.Context())
	require.NoError(t, err, "canceled wait cannot leak a generation slot")
	release()
}
