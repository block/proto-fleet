package rewriter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
)

func TestProtocolFromURL(t *testing.T) {
	cases := []struct {
		url  string
		want pb.PoolProtocol
		err  bool
	}{
		{"stratum+tcp://pool.example.com:3333", pb.PoolProtocol_POOL_PROTOCOL_SV1, false},
		{"stratum+ssl://pool.example.com:3333", pb.PoolProtocol_POOL_PROTOCOL_SV1, false},
		{"stratum+ws://pool.example.com:3333", pb.PoolProtocol_POOL_PROTOCOL_SV1, false},
		{"STRATUM+TCP://pool.example.com:3333", pb.PoolProtocol_POOL_PROTOCOL_SV1, false},
		{"stratum2+tcp://pool.example.com:34254", pb.PoolProtocol_POOL_PROTOCOL_SV2, false},
		{"stratum2+ssl://pool.example.com:34254", pb.PoolProtocol_POOL_PROTOCOL_SV2, false},
		{"stratum2+tcp://pool.example.com:34254/pubkey", pb.PoolProtocol_POOL_PROTOCOL_SV2, false},
		{"  stratum2+tcp://pool.example.com:34254  ", pb.PoolProtocol_POOL_PROTOCOL_SV2, false},
		{"http://not-a-pool.com", pb.PoolProtocol_POOL_PROTOCOL_UNSPECIFIED, true},
		{"", pb.PoolProtocol_POOL_PROTOCOL_UNSPECIFIED, true},
		{"stratum+udp://", pb.PoolProtocol_POOL_PROTOCOL_UNSPECIFIED, true},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			got, err := ProtocolFromURL(tc.url)
			if tc.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
