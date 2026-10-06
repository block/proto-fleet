package discovery

import (
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestSourceWarningRedactsInternalDiagnostics(t *testing.T) {
	for _, err := range []error{
		fleeterror.NewInternalError("SQLSTATE 23505: private schema"),
		fmt.Errorf("resolver setup: %w", fleeterror.NewInternalError("private interface")),
		connect.NewError(connect.CodeInternal, errors.New("private transport")),
		connect.NewError(connect.CodeUnknown, errors.New("private transport")),
		errors.New("private storage"),
	} {
		t.Run(err.Error(), func(t *testing.T) {
			require.Equal(t, "Fleet Server: discovery failed", SourceWarning("Fleet Server", err).Warning)
		})
	}
}

func TestSourceWarningRetainsActionableErrors(t *testing.T) {
	for _, err := range []error{
		fleeterror.NewInvalidArgumentError("invalid scan target"),
		connect.NewError(connect.CodeInvalidArgument, errors.New("invalid scan target")),
	} {
		require.Equal(t, "Fleet Node: invalid scan target", SourceWarning("Fleet Node", err).Warning)
	}
}
