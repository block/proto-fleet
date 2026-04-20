package testingtools

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
	"github.com/sourcegraph/jsonrpc2"
)

const (
	authorizeMethod = "mining.authorize"
)

type FakeStratumService struct {
	mu     sync.Mutex
	expect *fakeAuthorizeExpectations
}

func NewFakeStratumService() *FakeStratumService {
	fake := &FakeStratumService{}
	fake.expect = &fakeAuthorizeExpectations{
		fake:                  fake,
		authorizeExpectations: []fakeAuthorizeExpectation{},
	}
	return fake
}

type fakeAuthorizeExpectations struct {
	fake                  *FakeStratumService
	authorizeExpectations []fakeAuthorizeExpectation
}

type fakeAuthorizeExpectation struct {
	Username string
	Password *secrets.Text
	Return   bool
	Err      error
	Times    int
	called   int
}

//nolint:revive //It is intentional that it returns a private object type.
func (f *FakeStratumService) EXPECT() *fakeAuthorizeExpectations {
	return f.expect
}

func (f *FakeStratumService) Handle(ctx context.Context, c *jsonrpc2.Conn, req *jsonrpc2.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch req.Method {
	case authorizeMethod:
		var authReq authorizeRequest
		if err := authReq.UnmarshalParams(req.Params); err != nil {
			err := c.ReplyWithError(ctx, req.ID, jsonrpc2Error(jsonrpc2.CodeInvalidParams, err))
			if err != nil {
				slog.Error("Unable to return error", "error", err)
			}
			return
		}
		var password *string
		if authReq.password != nil {
			password = authReq.password
		}
		ret, err := f.Authorize(authReq.username, password)
		if err != nil {
			err := c.ReplyWithError(ctx, req.ID, jsonrpc2Error(jsonrpc2.CodeInternalError, err))
			if err != nil {
				slog.Error("Unable to return error", "error", err)
			}
			return
		}
		err = c.Reply(ctx, req.ID, ret)
		if err != nil {
			slog.Error("Unable to return", "error", err)
		}
		return
	default:
		err := c.ReplyWithError(ctx, req.ID, jsonrpc2Error(jsonrpc2.CodeMethodNotFound, fmt.Errorf("method %s not found", req.Method)))
		if err != nil {
			slog.Error("Unable to return error", "error", err)
		}
		return
	}
}

func (f *FakeStratumService) ValidateExpectations() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i := range f.expect.authorizeExpectations {
		exp := &f.expect.authorizeExpectations[i]
		if exp.called < exp.Times {
			return &ExpectationError{
				Method:   authorizeMethod,
				Expected: exp.Times,
				Called:   exp.called,
			}
		}
	}

	return nil
}

type ExpectationError struct {
	Method   string
	Expected int
	Called   int
}

func (e *ExpectationError) Error() string {
	return fmt.Sprintf("Expectation not met for method %s: expected %d, called %d", e.Method, e.Expected, e.Called)
}

func jsonrpc2Error(code int64, err error) *jsonrpc2.Error {
	ret := &jsonrpc2.Error{
		Code: code,
	}
	ret.SetError(err)
	return ret
}
