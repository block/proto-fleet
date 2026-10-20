package hostedaccounts_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/grpc/hosted/v1/hostedv1connect"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/session"
	hostedhandler "github.com/block/proto-fleet/server/internal/handlers/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/stretchr/testify/require"
)

type heldActivityExportWriter struct {
	http.ResponseWriter
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (w *heldActivityExportWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
	w.once.Do(func() { close(w.entered); <-w.release })
}

func TestHostedActivityStreamReauthorizesBetweenChunks(t *testing.T) {
	f := newRPCFixture(t)
	a := f.account(t, "Customer", f.host, domain.AllCapabilities)
	viewer := f.addUser(t, "stream-viewer", false)
	require.NoError(t, f.accounts.SetMembership(t.Context(), f.host, f.org, a.AccountID, viewer, domain.RoleViewer))
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO activity_log(event_id,organization_id,event_category,event_type,description,result,actor_type,metadata) SELECT gen_random_uuid(),$1,'auth','create_hosted_account_pool','private audit text','success','user',jsonb_build_object('account_id',$2::text,'password','secret') FROM generate_series(1,101)`, f.org, a.AccountID)
	require.NoError(t, err)
	entered, proceed := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(proceed) })
	_, handler := hostedv1connect.NewHostedAccountServiceHandler(hostedhandler.NewHandler(f.accounts, nil, nil, nil, nil, nil), connect.WithInterceptors(interceptors.NewErrorMappingInterceptor()))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := authn.SetInfo(r.Context(), &session.Info{UserID: viewer, OrganizationID: f.org, AuthMethod: session.AuthMethodSession})
		handler.ServeHTTP(&heldActivityExportWriter{ResponseWriter: w, entered: entered, release: proceed}, r.WithContext(ctx))
	}))
	t.Cleanup(server.Close)
	t.Cleanup(release)
	client := hostedv1connect.NewHostedAccountServiceClient(server.Client(), server.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	stream, err := client.ExportAccountActivities(ctx, connect.NewRequest(&pb.ExportAccountActivitiesRequest{AccountId: a.AccountID}))
	require.NoError(t, err)
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("export did not flush its first chunk")
	}
	require.True(t, stream.Receive())
	require.Contains(t, string(stream.Msg().Chunk), "Created pool configuration")
	require.NotContains(t, string(stream.Msg().Chunk), "private audit text")
	require.NoError(t, f.accounts.RevokeMembership(t.Context(), f.host, f.org, a.AccountID, viewer))
	release()
	require.False(t, stream.Receive(), "revocation between chunks must stop the remaining export")
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(stream.Err()))
}
