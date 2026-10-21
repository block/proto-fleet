package hostedaccounts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const (
	invitationClientAttemptLimit = 60
	// Eight-character codes rely on this hourly ceiling: a 24-hour code faces
	// at most 24,000 guesses however many clients an attacker controls.
	invitationDeploymentAttemptLimit = 1000
)

// CheckClientRateLimit admits one public preview or redemption request. The
// handler supplies the trusted middleware address; internal service calls do
// not spend additional requests for the same registration flow. A request
// spends its client's per-minute allowance first, so a throttled client never
// consumes the deployment-wide hourly allowance.
func (s *InvitationService) CheckClientRateLimit(ctx context.Context, ipAddress string) error {
	ip, err := netip.ParseAddr(ipAddress)
	if err != nil || ip.Zone() != "" {
		return fleeterror.NewFailedPreconditionError("invitation client address unavailable")
	}
	ip = ip.Unmap()
	client := ip.String()
	if ip.Is6() {
		// One host must not reset its allowance by rotating IPv6 addresses.
		client = netip.PrefixFrom(ip, 64).Masked().String()
	}
	sum := sha256.Sum256([]byte(client))
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	charged, err := s.accounts.queries.ConsumeHostedInvitationRateLimit(ctx, sqlc.ConsumeHostedInvitationRateLimitParams{
		ClientHash: hex.EncodeToString(sum[:]), AttemptLimit: invitationClientAttemptLimit,
	})
	if err != nil {
		return err
	}
	if charged != 1 {
		return fleeterror.NewResourceExhaustedErrorf("invitation requests temporarily limited; try again later")
	}
	charged, err = s.accounts.queries.ConsumeHostedInvitationDeploymentRateLimit(ctx, invitationDeploymentAttemptLimit)
	if err != nil {
		return err
	}
	if charged != 1 {
		return fleeterror.NewResourceExhaustedErrorf("invitation requests temporarily limited; try again later")
	}
	// Admission commits first. Cleanup cannot refund a request or hold its lock
	// while acquiring another client's row.
	return s.accounts.queries.PruneHostedInvitationRateLimit(ctx)
}
