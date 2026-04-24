package rewriter

import (
	"fmt"
	"strings"

	pb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
)

// ProtocolFromURL maps a pool URL's scheme to the PoolProtocol enum.
// The URL is the single source of truth for protocol — stratum+(tcp|ssl|ws)
// is SV1, stratum2+(tcp|ssl) is SV2, anything else is a validation error.
// Shape validation (host/port/path) is the proto-level CEL rule's job;
// this function only inspects the scheme prefix.
//
// Returned alongside an error so callers that receive a pool URL from
// an untrusted source (RPC requests) can reject cleanly rather than
// silently defaulting to SV1 on a malformed input.
func ProtocolFromURL(url string) (pb.PoolProtocol, error) {
	lower := strings.ToLower(strings.TrimSpace(url))
	switch {
	case strings.HasPrefix(lower, "stratum2+tcp://"),
		strings.HasPrefix(lower, "stratum2+ssl://"):
		return pb.PoolProtocol_POOL_PROTOCOL_SV2, nil
	case strings.HasPrefix(lower, "stratum+tcp://"),
		strings.HasPrefix(lower, "stratum+ssl://"),
		strings.HasPrefix(lower, "stratum+ws://"):
		return pb.PoolProtocol_POOL_PROTOCOL_SV1, nil
	default:
		return pb.PoolProtocol_POOL_PROTOCOL_UNSPECIFIED,
			fmt.Errorf("pool URL has no recognised stratum scheme: %q", url)
	}
}

// MustProtocolFromURL is ProtocolFromURL with the error swallowed as
// SV1 — for code paths where the URL has already been CEL-validated
// upstream and a scheme mismatch would be a programmer bug rather than
// a runtime concern. Still logs nothing; it's an assertion, not a
// recovery path.
func MustProtocolFromURL(url string) pb.PoolProtocol {
	p, err := ProtocolFromURL(url)
	if err != nil {
		return pb.PoolProtocol_POOL_PROTOCOL_SV1
	}
	return p
}
