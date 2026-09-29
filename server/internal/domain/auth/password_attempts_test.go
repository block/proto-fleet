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
			if limiter.allow("user", now) {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()
	if got := allowed.Load(); got != 10 {
		t.Fatalf("allowed %d, want 10", got)
	}
	if !limiter.allow("User", now) {
		t.Fatal("must preserve exact username semantics")
	}
	if limiter.allow("user", now.Add(time.Minute-time.Nanosecond)) {
		t.Fatal("expired early")
	}
	if !limiter.allow("user", now.Add(time.Minute)) {
		t.Fatal("did not expire")
	}
}

func TestPasswordLimitCoversEveryVerificationPath(t *testing.T) {
	// No stores are needed: an exhausted limit must reject before any lookup.
	service := &Service{}
	for range 10 {
		if err := service.checkPasswordAttempt("admin"); err != nil {
			t.Fatal(err)
		}
	}
	ctx := ctxWithSession("user-1", "admin", 1)
	_, _, loginErr := service.AuthenticateUser(context.Background(), &authv1.AuthenticateRequest{Username: "admin", Password: "password"}, "", "")
	verifyErr := service.VerifyCredentials(ctx, "admin", "password")
	// A different submitted username must not bypass the session user's limit.
	stepUpErr := service.VerifySessionCredentials(ctx, "someone-else", "password")
	_, updateErr := service.UpdatePassword(ctx, &authv1.UpdatePasswordRequest{CurrentPassword: "old", NewPassword: "NewPassword123!"}, "", "")
	for _, err := range []error{loginErr, verifyErr, stepUpErr, updateErr} {
		if connect.CodeOf(err) != connect.CodeResourceExhausted {
			t.Fatalf("got %v, want resource exhausted", err)
		}
	}
}

func TestPasswordLimitCountsSuccessesAndUnknownUsers(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{userStore: &mockUserStoreForVerify{users: map[string]interfaces.User{
		"known": {Username: "known", PasswordHash: string(hash)},
	}}}
	for range 10 {
		if err := service.VerifyCredentials(context.Background(), "known", "password"); err != nil {
			t.Fatal(err)
		}
		_, _, err := service.AuthenticateUser(context.Background(), &authv1.AuthenticateRequest{Username: "unknown", Password: "password"}, "", "")
		if err == nil || connect.CodeOf(err) == connect.CodeResourceExhausted {
			t.Fatalf("unexpected early limit: %v", err)
		}
	}
	for _, username := range []string{"known", "unknown"} {
		if err := service.VerifyCredentials(context.Background(), username, "password"); connect.CodeOf(err) != connect.CodeResourceExhausted {
			t.Fatalf("%s: %v", username, err)
		}
	}
}

func TestPasswordAttemptsCapacity(t *testing.T) {
	var limiter passwordAttempts
	now := time.Unix(1000, 0)
	for i := range 10000 {
		if !limiter.allow(fmt.Sprint(i), now) {
			t.Fatal(i)
		}
	}
	if limiter.allow("overflow", now) {
		t.Fatal("evicted a live entry")
	}
	if !limiter.allow("0", now) {
		t.Fatal("full cache blocked existing identity")
	}
	if !limiter.allow("overflow", now.Add(time.Minute)) {
		t.Fatal("did not reclaim expired entries")
	}
}
