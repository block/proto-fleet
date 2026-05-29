package auth

import (
	"context"

	"connectrpc.com/authn"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Subject is the fleet node identity placed on ctx by FleetNodeAuthInterceptor.
type Subject struct {
	FleetNodeID         int64
	OrgID               int64
	Name                string
	IdentityFingerprint string
}

func GetSubject(ctx context.Context) (*Subject, error) {
	sub, ok := authn.GetInfo(ctx).(*Subject)
	if !ok {
		return nil, fleeterror.NewInternalError(
			"context does not have fleet node subject; route is not under FleetNodeAuthInterceptor",
		)
	}
	return sub, nil
}
