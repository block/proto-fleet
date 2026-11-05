package poolconfigurations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/poolconfigurations/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/google/uuid"
)

// AuthorizeWorkerNameReviews verifies the password once for a frozen selection.
// It only grants credential approval: confirmation still admits each review and
// rechecks live permissions, physical bindings, maintenance, and credentials.
func (s *Service) AuthorizeWorkerNameReviews(ctx context.Context, reviewIDs []string, password string) error {
	if len(reviewIDs) == 0 || len(reviewIDs) > 10000 || password == "" || utf8.RuneCountInString(password) > 1024 {
		return fleeterror.NewInvalidArgumentError("select between 1 and 10000 worker-name reviews and provide your password")
	}
	ids := slices.Clone(reviewIDs)
	slices.Sort(ids)
	for i, id := range ids {
		if _, err := uuid.Parse(id); err != nil || (i > 0 && ids[i-1] == id) {
			return fleeterror.NewInvalidArgumentError("worker-name review IDs must be unique UUIDs")
		}
	}
	// Reject unauthorized/expired/non-worker reviews before spending password budget.
	_, err := withAuthority(ctx, s, "", Read, func(q sqlc.Querier, p Principal) (struct{}, error) {
		if _, err := reviewSessionHash(ctx, p); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, validateWorkerNameReviews(ctx, q, p, ids)
	})
	if err != nil {
		return err
	}
	if s.equipment == nil {
		return fleeterror.NewInternalError("equipment confirmation is unavailable")
	}
	proof, err := s.equipment.VerifyPoolEquipmentCredentials(ctx, "", password)
	if err != nil {
		return err
	}
	if proof == nil {
		return fleeterror.NewInternalError("equipment credential proof is unavailable")
	}
	_, err = withAuthority(ctx, s, "", Read, func(q sqlc.Querier, p Principal) (struct{}, error) {
		sessionHash, err := reviewSessionHash(ctx, p)
		if err != nil {
			return struct{}{}, err
		}
		if err := proof(ctx, q); err != nil {
			return struct{}{}, err
		}
		if err := validateWorkerNameReviews(ctx, q, p, ids); err != nil {
			return struct{}{}, err
		}
		user, err := q.GetUserById(ctx, p.ActorID)
		if err != nil {
			return struct{}{}, err
		}
		count, err := q.AuthorizePoolConfigurationReviews(ctx, sqlc.AuthorizePoolConfigurationReviewsParams{
			OrganizationID: p.Scope.OrganizationID, ActorID: p.ActorID, ReviewIds: ids, SessionHash: sessionHash, CredentialFingerprint: reviewCredentialFingerprint(user),
		})
		if err != nil {
			return struct{}{}, err
		}
		if count != int64(len(ids)) {
			return struct{}{}, fleeterror.NewInternalError("worker-name authorization omitted reviews")
		}
		return struct{}{}, nil
	})
	return err
}

func validateWorkerNameReviews(ctx context.Context, q sqlc.Querier, p Principal, ids []string) error {
	if err := authorizeWorkerNames(ctx, q, p.Scope, p.ActorID); err != nil {
		return err
	}
	frozen, err := q.PoolConfigurationWritersFrozen(ctx, p.Scope.OrganizationID)
	if err != nil {
		return err
	}
	if frozen {
		return fleeterror.NewFailedPreconditionError("Pool configuration maintenance is in progress.")
	}
	for _, id := range ids {
		review, err := q.LockPoolConfigurationReview(ctx, sqlc.LockPoolConfigurationReviewParams{OrganizationID: p.Scope.OrganizationID, AccountID: p.Scope.account(), ActorID: p.ActorID, ReviewID: id})
		if err != nil {
			return storageError(err)
		}
		// Batch authorization is not confirmation. Do not return ConfirmReview's
		// definitive-expiry detail for a whole batch with potentially mixed outcomes.
		if !review.Valid && !review.SavedVersion.Valid {
			return staleReview()
		}
		var targets []reviewedTarget
		if json.Unmarshal(review.Targets, &targets) != nil {
			return fleeterror.NewInternalError("decode pool review failed")
		}
		if review.Kind != "assign" || len(workerNameChanges(targets)) == 0 {
			return fleeterror.NewInvalidArgumentError("batch authorization requires worker-name reviews")
		}
	}
	return nil
}

// The caller holds the review lock and has already checked acceptance and expiry.
func checkReviewAuthorization(ctx context.Context, q sqlc.Querier, p Principal, reviewID string) error {
	sessionHash, err := reviewSessionHash(ctx, p)
	if err != nil {
		return err
	}
	// withAuthority retains the actor lock while this fingerprint is checked and
	// the canonical operation is committed; a password change cannot race it.
	user, err := q.GetUserById(ctx, p.ActorID)
	if err != nil {
		return err
	}
	approved, err := q.HasPoolConfigurationReviewAuthorization(ctx, sqlc.HasPoolConfigurationReviewAuthorizationParams{
		OrganizationID: p.Scope.OrganizationID, ActorID: p.ActorID, ReviewID: reviewID, SessionHash: sessionHash, CredentialFingerprint: reviewCredentialFingerprint(user),
	})
	if err != nil {
		return err
	}
	if !approved {
		return fleeterror.NewErrorWithEndpointCode("Verify your password to confirm these changes.", connect.CodeFailedPrecondition, int32(pb.ConfirmReviewErrorCode_CONFIRM_REVIEW_ERROR_CODE_AUTHORIZATION_REQUIRED))
	}
	return nil
}

func reviewSessionHash(ctx context.Context, p Principal) (string, error) {
	info, err := session.GetInfo(ctx)
	if err != nil || info.AuthMethod != session.AuthMethodSession || info.SessionID == "" || info.AccountKey || info.Actor != "" || info.UserID != p.ActorID || info.OrganizationID != p.Scope.OrganizationID {
		return "", fleeterror.NewForbiddenError("worker-name approval requires the initiating user session")
	}
	return reviewHash([]byte(info.SessionID)), nil
}

func reviewCredentialFingerprint(user sqlc.User) string {
	// Canonical strings produce the same fingerprint across replicas/time zones.
	// Store a digest, never the password hash or password, in the approval record.
	updatedAt := ""
	if user.PasswordUpdatedAt.Valid {
		updatedAt = user.PasswordUpdatedAt.Time.UTC().Format(time.RFC3339Nano)
	}
	// PostgreSQL text cannot contain NUL, so these field boundaries are unambiguous.
	return reviewHash([]byte(user.Username + "\x00" + user.PasswordHash + "\x00" + updatedAt))
}

func reviewHash(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
