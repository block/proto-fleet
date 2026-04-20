package testingtools

import (
	"encoding/json"
	"fmt"

	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
)

type authorizeRequest struct {
	username string
	password *string
}

func (a *authorizeRequest) UnmarshalParams(params *json.RawMessage) error {
	if params == nil {
		return fmt.Errorf("params cannot be nil")
	}
	values := []string{}
	if err := json.Unmarshal(*params, &values); err != nil {
		return fmt.Errorf("failed to unmarshal params: %w", err)
	}
	if len(values) > 2 || len(values) == 0 {
		return fmt.Errorf("expected 1 or 2 params but got: %d", len(values))
	}
	a.username = values[0]
	if len(values) == 2 {
		pass := values[1]
		a.password = &pass
	}
	return nil
}

type authorizeCallBuilder struct {
	expectation *fakeAuthorizeExpectation
	fake        *FakeStratumService
}

func (b *authorizeCallBuilder) Return(ret bool, err error) *authorizeCallBuilder {
	b.expectation.Return = ret
	b.expectation.Err = err
	return b
}
func (b *authorizeCallBuilder) Times(times int) *authorizeCallBuilder {
	b.expectation.Times = times
	return b
}

func (ex *fakeAuthorizeExpectations) Authorize(username string, password *secrets.Text) *authorizeCallBuilder {
	exp := fakeAuthorizeExpectation{
		Username: username,
		Password: password,
		Times:    1,
	}
	ex.authorizeExpectations = append(ex.authorizeExpectations, exp)
	// Ensure the expectation is always at the end of the slice
	idx := len(ex.authorizeExpectations) - 1
	return &authorizeCallBuilder{
		expectation: &ex.authorizeExpectations[idx],
		fake:        ex.fake,
	}
}

func (f *FakeStratumService) Authorize(username string, password *string) (bool, error) {
	for i := range f.expect.authorizeExpectations {
		exp := &f.expect.authorizeExpectations[i]
		if exp.Username == username && (exp.Password == nil || exp.Password.Value() == *password) {
			exp.called++
			if exp.called > exp.Times {
				return false, nil // Exceeded expected calls
			}
			return exp.Return, exp.Err
		}
	}

	return false, nil // No matching expectation found
}
