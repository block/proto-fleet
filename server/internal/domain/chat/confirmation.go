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
	decision       chan ConfirmationResolution
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
) (ConfirmationResolution, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return ConfirmationResolution{}, fleeterror.NewUnauthenticatedError("authentication required")
	}
	confirmationID := uuid.NewString()
	pending := &pendingConfirmation{
		organizationID: info.OrganizationID,
		userID:         info.UserID,
		decision:       make(chan ConfirmationResolution, 1),
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
		return ConfirmationResolution{}, err
	}

	timer := time.NewTimer(b.ttl)
	defer timer.Stop()
	select {
	case resolution := <-pending.decision:
		// Keep the stream lifetime, but execute with identity and permissions
		// loaded for the approval RPC. Its short-lived context may already be
		// canceled by the time the agent resumes.
		resolution.Context = confirmationExecutionContext{Context: ctx, values: resolution.Context}
		return resolution, nil
	case <-timer.C:
		return ConfirmationResolution{}, fleeterror.NewPlainError("tool confirmation expired", connect.CodeDeadlineExceeded)
	case <-ctx.Done():
		return ConfirmationResolution{}, fmt.Errorf("wait for tool confirmation: %w", ctx.Err())
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
	case pending.decision <- ConfirmationResolution{Decision: decision, Context: ctx}:
		return nil
	default:
		return fleeterror.NewFailedPreconditionError("tool confirmation was already resolved")
	}
}

// confirmationExecutionContext retains the original stream's cancellation and
// deadline while taking request values from the freshly authenticated approval.
type confirmationExecutionContext struct {
	context.Context                 //nolint:containedctx // Implements the stream lifetime of the execution context.
	values          context.Context //nolint:containedctx // Approval request values live only for this confirmation.
}

func (c confirmationExecutionContext) Value(key any) any {
	return c.values.Value(key)
}
