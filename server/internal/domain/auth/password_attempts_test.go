package auth

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	authv1 "github.com/block/proto-fleet/server/generated/grpc/auth/v1"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"golang.org/x/crypto/bcrypt"
)

func TestPasswordAttemptsBoundary(t *testing.T) {
	var limiter passwordAttempts
	now := time.Unix(1000, 0)
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for range 40 {
		wg.Go(func() {
			if limiter.allow(1, now) {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()
	if got := allowed.Load(); got != 10 {
		t.Fatalf("allowed %d, want 10", got)
	}
	if !limiter.allow(2, now) {
		t.Fatal("must preserve independent account budgets")
	}
	if limiter.allow(1, now.Add(time.Minute-time.Nanosecond)) {
		t.Fatal("expired early")
	}
	if !limiter.allow(1, now.Add(time.Minute)) {
		t.Fatal("did not expire")
	}
}

func TestPasswordLimitSurvivesRenameAcrossEveryVerificationPath(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	store := &mockUserStoreForVerify{users: map[string]interfaces.User{
		"admin": {ID: 1, Username: "admin", PasswordHash: string(hash)},
	}}
	service := &Service{userStore: store}
	for range 10 {
		if err := service.VerifyCredentials(context.Background(), "admin", "password"); err != nil {
			t.Fatal(err)
		}
	}
	ctx := ctxWithSession("user-1", "admin", 1)
	if err := service.UpdateUsername(ctx, "renamed"); err != nil {
		t.Fatal(err)
	}
	ctx = ctxWithSession("user-1", "renamed", 1)
	_, _, loginErr := service.AuthenticateUser(context.Background(), &authv1.AuthenticateRequest{Username: "renamed", Password: "password"}, "", "")
	verifyErr := service.VerifyCredentials(ctx, "renamed", "password")
	// A different submitted username must not bypass the session user's limit.
	stepUpErr := service.VerifySessionCredentials(ctx, "someone-else", "password")
	_, updateErr := service.UpdatePassword(ctx, &authv1.UpdatePasswordRequest{CurrentPassword: "old", NewPassword: "NewPassword123!"}, "", "")
	for _, err := range []error{loginErr, verifyErr, stepUpErr, updateErr} {
		if connect.CodeOf(err) != connect.CodeResourceExhausted {
			t.Fatalf("got %v, want resource exhausted", err)
		}
	}
}

func TestUnknownUserFloodCannotConsumeAccountBudget(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{userStore: &mockUserStoreForVerify{users: map[string]interfaces.User{
		"known": {ID: 1, Username: "known", PasswordHash: string(hash)},
	}}}
	for i := range 10001 {
		username := fmt.Sprintf("unknown-%d", i)
		_, _, err := service.AuthenticateUser(context.Background(), &authv1.AuthenticateRequest{Username: username, Password: "password"}, "", "")
		if err == nil || connect.CodeOf(err) == connect.CodeResourceExhausted {
			t.Fatalf("unexpected unknown-user response: %v", err)
		}
		if err := service.VerifyCredentials(context.Background(), username, "password"); err == nil || connect.CodeOf(err) == connect.CodeResourceExhausted {
			t.Fatalf("unexpected unknown-user verification: %v", err)
		}
	}
	if err := service.VerifyCredentials(context.Background(), "known", "password"); err != nil {
		t.Fatalf("unknown-user flood blocked known account: %v", err)
	}
}

func TestPasswordAttemptsCapacity(t *testing.T) {
	var limiter passwordAttempts
	now := time.Unix(1000, 0)
	for i := range 10000 {
		if !limiter.allow(int64(i), now) {
			t.Fatal(i)
		}
	}
	if limiter.allow(10000, now) {
		t.Fatal("evicted a live entry")
	}
	if !limiter.allow(0, now) {
		t.Fatal("full cache blocked existing identity")
	}
	if !limiter.allow(10000, now.Add(time.Minute)) {
		t.Fatal("did not reclaim expired entries")
	}
}
