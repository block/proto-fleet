package chat

import (
	"context"
	"fmt"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

const defaultConfirmationTTL = 5 * time.Minute

type pendingConfirmation struct {
	organizationID int64
	userID         int64
	decision       chan ConfirmationDecision
}

// ConfirmationBroker coordinates a short-lived approval RPC with the
// SendMessage stream that requested the write. Pending confirmations are
// deliberately process-local and live only as long as the original stream.
type ConfirmationBroker struct {
	mu      sync.Mutex
	pending map[string]*pendingConfirmation
	ttl     time.Duration
}

func NewConfirmationBroker(ttl ...time.Duration) *ConfirmationBroker {
	confirmationTTL := defaultConfirmationTTL
	if len(ttl) > 0 {
		confirmationTTL = ttl[0]
	}
	return &ConfirmationBroker{
		pending: make(map[string]*pendingConfirmation),
		ttl:     confirmationTTL,
	}
}

func (b *ConfirmationBroker) Await(
	ctx context.Context,
	_ ConfirmationRequest,
	notify func(confirmationID string) error,
) (ConfirmationDecision, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return "", fleeterror.NewUnauthenticatedError("authentication required")
	}
	confirmationID := uuid.NewString()
	pending := &pendingConfirmation{
		organizationID: info.OrganizationID,
		userID:         info.UserID,
		decision:       make(chan ConfirmationDecision, 1),
	}

	b.mu.Lock()
	b.pending[confirmationID] = pending
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.pending, confirmationID)
		b.mu.Unlock()
	}()

	if err := notify(confirmationID); err != nil {
		return "", err
	}

	timer := time.NewTimer(b.ttl)
	defer timer.Stop()
	select {
	case decision := <-pending.decision:
		return decision, nil
	case <-timer.C:
		return "", fleeterror.NewPlainError("tool confirmation expired", connect.CodeDeadlineExceeded)
	case <-ctx.Done():
		return "", fmt.Errorf("wait for tool confirmation: %w", ctx.Err())
	}
}

func (b *ConfirmationBroker) Resolve(ctx context.Context, confirmationID string, decision ConfirmationDecision) error {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return fleeterror.NewUnauthenticatedError("authentication required")
	}
	if decision != ConfirmationApproved && decision != ConfirmationCancelled {
		return fleeterror.NewInvalidArgumentError("invalid tool confirmation decision")
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	pending, ok := b.pending[confirmationID]
	if !ok || pending.organizationID != info.OrganizationID || pending.userID != info.UserID {
		return fleeterror.NewNotFoundError("tool confirmation not found or expired")
	}
	select {
	case pending.decision <- decision:
		return nil
	default:
		return fleeterror.NewFailedPreconditionError("tool confirmation was already resolved")
	}
}
