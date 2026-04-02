package testingtools

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFakeStratumService_Authorize_Expectations(t *testing.T) {
	numberCalls := 3
	fake := NewFakeStratumService()
	secretPwd := secrets.NewText("password123")

	// Add an expectation: username "user1", password "password123", should return true, no error, expected 2 times
	fake.EXPECT().
		Authorize("user1", secretPwd).
		Return(true, nil).
		Times(numberCalls)

	// Simulate Authorize calls
	for i := range numberCalls {
		ret, err := fake.Authorize("user1", SecretToStringPtr(secretPwd))
		require.NoError(t, err, "unexpected error on call %d", i+1)
		assert.True(t, ret, "expected true on call %d", i+1)
	}

	// Validate expectations should pass
	assert.NoError(t, fake.ValidateExpectations(), "expected no error validating expectations")
}

func TestFakeStratumService_Authorize_ExpectationNotMet(t *testing.T) {
	numberCalls := 3
	fake := NewFakeStratumService()
	secretPwd := secrets.NewText("password123")

	// Add an expectation: username "user2", password "password123", should return false, no error, expected 1 time
	fake.EXPECT().
		Authorize("user2", secretPwd).
		Return(false, nil).
		Times(numberCalls)

	// Only call once (should be called twice)
	ret, err := fake.Authorize("user2", SecretToStringPtr(secretPwd))
	require.NoError(t, err, "unexpected error on call")
	assert.False(t, ret, "expected false on first call")

	// Validate expectations should fail
	assert.Error(t, fake.ValidateExpectations(), "expected error validating expectations due to unmet expectation")
}

func TestExpectationError_Error(t *testing.T) {
	err := &ExpectationError{
		Method:   "mining.authorize",
		Expected: 3,
		Called:   1,
	}
	expectedMsg := "Expectation not met for method mining.authorize: expected 3, called 1"
	require.Equal(t, expectedMsg, err.Error(), "Error message should match expected format")
}

func SecretToStringPtr(s *secrets.Text) *string {
	if s == nil {
		return nil
	}
	str := s.Value()
	return &str
}
