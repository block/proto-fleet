package deployment

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
)

func TestEtcdSpaceWarnings(t *testing.T) {
	for _, test := range []struct {
		name              string
		size, used, quota int64
		errors            []string
		want              bool
	}{
		{name: "healthy", size: 69, used: 40, quota: 100},
		{name: "allocated warning boundary", size: 70, used: 40, quota: 100, want: true},
		{name: "live data pressure", size: 90, used: 89, quota: 100, want: true},
		{name: "unknown quota", size: 1, used: 1, want: true},
		{name: "alarm", size: 50, used: 40, quota: 100, errors: []string{"NOSPACE"}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			member := etcdMemberSpace("https://member:2379", &clientv3.StatusResponse{DbSize: test.size, DbSizeInUse: test.used, DbSizeQuota: test.quota, Errors: test.errors})
			require.True(t, member.Available)
			require.Equal(t, test.want, member.Warning)
			require.Equal(t, test.size, member.DBSize)
			require.Equal(t, test.used, member.DBSizeInUse)
			require.Equal(t, test.quota, member.DBSizeQuota)
		})
	}
}

// Exercise the actual RPC probe, including an unresponsive member. A healthy
// majority must not hide a member that cannot report its capacity.
func TestEtcdProbeRequiresCapacityFromEveryMember(t *testing.T) {
	for _, test := range []struct {
		name        string
		size        int64
		unavailable bool
		want        bool
	}{
		{name: "healthy", size: 50, want: true},
		{name: "one member pressure", size: 70},
		{name: "one member timeout", size: 50, unavailable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoints := make([]string, 0, 3)
			for i, id := range []uint64{1, 2, 3} {
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				require.NoError(t, err)
				server := grpc.NewServer()
				fake := &spaceTestServer{id: id, size: 50}
				if i == 2 {
					fake.size = test.size
					fake.unavailable = test.unavailable
				}
				etcdserverpb.RegisterMaintenanceServer(server, fake)
				etcdserverpb.RegisterKVServer(server, fake)
				go func() { _ = server.Serve(listener) }()
				t.Cleanup(server.Stop)
				endpoints = append(endpoints, "http://"+listener.Addr().String())
			}
			result := probeEtcdMembers(t.Context(), clientv3.Config{Endpoints: endpoints, DialTimeout: time.Second})
			require.Equal(t, test.want || test.unavailable, result.spaceHealthy)
			require.Equal(t, test.want, result.redundant && result.spaceHealthy)
			require.True(t, result.quorum)
			require.Len(t, result.members, 3)
			for _, member := range result.members {
				if member.Endpoint == endpoints[2] {
					require.Equal(t, !test.want, member.Warning)
					require.Equal(t, !test.unavailable, member.Available)
				}
			}
			if test.unavailable {
				require.False(t, result.redundant)
			}
		})
	}
}

type spaceTestServer struct {
	etcdserverpb.UnimplementedMaintenanceServer
	etcdserverpb.UnimplementedKVServer
	id          uint64
	size        int64
	unavailable bool
}

func (s *spaceTestServer) Status(ctx context.Context, _ *etcdserverpb.StatusRequest) (*etcdserverpb.StatusResponse, error) {
	if s.unavailable {
		<-ctx.Done()
		return nil, fmt.Errorf("probe canceled: %w", ctx.Err())
	}
	return &etcdserverpb.StatusResponse{Header: &etcdserverpb.ResponseHeader{ClusterId: 1, MemberId: s.id}, DbSize: s.size, DbSizeInUse: 40, DbSizeQuota: 100}, nil
}
func (s *spaceTestServer) Range(context.Context, *etcdserverpb.RangeRequest) (*etcdserverpb.RangeResponse, error) {
	return &etcdserverpb.RangeResponse{Header: &etcdserverpb.ResponseHeader{ClusterId: 1, MemberId: s.id}}, nil
}
