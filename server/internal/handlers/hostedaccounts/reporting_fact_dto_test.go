package hostedaccounts

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/grpc/hosted/v1/hostedv1connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestReportingFactMutationsRejectMismatchedTypedKeys(t *testing.T) {
	order := &pb.ReportingFact{Value: &pb.ReportingFact_Order{Order: &pb.ReportingOrder{Id: "order", Name: "Hosting", Start: timestamppb.New(time.Now()), SourceRef: "Signed order"}}}
	valid := &pb.ReportingFactMutation{Kind: pb.ReportingFactKind_REPORTING_FACT_KIND_ORDER, Id: "order", Change: &pb.ReportingFactMutation_Upsert{Upsert: order}}
	got, err := reportingFactMutationDomain(valid)
	require.NoError(t, err)
	require.Equal(t, "order", got.ID)
	require.False(t, got.Delete)
	for _, mutation := range []*pb.ReportingFactMutation{
		{Kind: pb.ReportingFactKind_REPORTING_FACT_KIND_METER, Id: "order", Change: valid.Change},
		{Kind: valid.Kind, Id: "different", Change: valid.Change},
		{Kind: valid.Kind, Id: "order", Change: &pb.ReportingFactMutation_Delete{Delete: false}},
		{Kind: valid.Kind, Id: "order"},
		{Kind: pb.ReportingFactKind(99), Id: "order", Change: valid.Change},
		{Kind: valid.Kind, Id: "order", Change: &pb.ReportingFactMutation_Upsert{}},
	} {
		_, err := reportingFactMutationDomain(mutation)
		require.Error(t, err)
	}
}

func TestReportingOperationRequestValidation(t *testing.T) {
	const account = "9fec2e20-51a8-43c9-8936-826655c363f0"
	deletion := &pb.ReportingFactMutation{Kind: pb.ReportingFactKind_REPORTING_FACT_KIND_ORDER, Id: "old", Change: &pb.ReportingFactMutation_Delete{Delete: true}}
	legal := &pb.MutateHostReportingInputsRequest{AccountId: account, ExpectedRevision: 1, SourceRef: "Reviewed changes", Mutations: []*pb.ReportingFactMutation{deletion}}
	require.NoError(t, protovalidate.Validate(legal))
	require.Error(t, protovalidate.Validate(&pb.MutateHostReportingInputsRequest{AccountId: account, SourceRef: "No operations"}))
	oversized := &pb.MutateHostReportingInputsRequest{AccountId: account, ExpectedRevision: 1, SourceRef: "Reviewed changes", Mutations: make([]*pb.ReportingFactMutation, 201)}
	for i := range oversized.Mutations {
		oversized.Mutations[i] = deletion
	}
	require.Error(t, protovalidate.Validate(oversized))
	for _, request := range []*pb.ListHostReportingFactsRequest{
		{AccountId: account, Kind: pb.ReportingFactKind_REPORTING_FACT_KIND_ORDER, ExpectedRevision: 1, PageSize: 200},
	} {
		require.NoError(t, protovalidate.Validate(request))
		request.PageSize = 201
		require.Error(t, protovalidate.Validate(request))
		request.PageSize = 200
		request.ExpectedRevision = 0
		require.Error(t, protovalidate.Validate(request))
		request.ExpectedRevision = 1
		request.Kind = pb.ReportingFactKind(99)
		require.Error(t, protovalidate.Validate(request))
	}
}

func TestReportingMutationReadLimitPrecedesHandlerDecoding(t *testing.T) {
	path, handler := hostedv1connect.NewHostedAccountServiceHandler(NewHandler(nil, nil, nil, nil, nil), ReportingRequestReadLimitOption())
	require.NotEmpty(t, path)
	server := httptest.NewServer(handler)
	defer server.Close()
	client := hostedv1connect.NewHostedAccountServiceClient(server.Client(), server.URL)
	_, err := client.MutateHostReportingInputs(context.Background(), connect.NewRequest(&pb.MutateHostReportingInputsRequest{SourceRef: strings.Repeat("x", reportingMutationReadMaxBytes+1)}))
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
}
