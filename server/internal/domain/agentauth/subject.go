package agentauth

import (
	"context"

	"connectrpc.com/authn"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Subject is the agent identity placed on ctx by AgentAuthInterceptor.
type Subject struct {
	AgentID             int64
	OrgID               int64
	Name                string
	IdentityFingerprint string
}

func GetSubject(ctx context.Context) (*Subject, error) {
	sub, ok := authn.GetInfo(ctx).(*Subject)
	if !ok {
		return nil, fleeterror.NewInternalError(
			"context does not have agent subject; route is not under AgentAuthInterceptor",
		)
	}
	return sub, nil
}
