package agentauth

import (
	"context"

	"connectrpc.com/authn"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Subject identifies an authenticated agent on a request context. Populated
// by AgentAuthInterceptor; retrieved by handlers via GetSubject.
type Subject struct {
	AgentID             int64
	OrgID               int64
	Name                string
	IdentityFingerprint string
}

// GetSubject extracts the agent subject from the context. Returns an internal
// error if the context was not authenticated as an agent (i.e. caller is on a
// procedure that didn't run AgentAuthInterceptor).
func GetSubject(ctx context.Context) (*Subject, error) {
	sub, ok := authn.GetInfo(ctx).(*Subject)
	if !ok {
		return nil, fleeterror.NewInternalError(
			"context does not have agent subject; route is not under AgentAuthInterceptor",
		)
	}
	return sub, nil
}
