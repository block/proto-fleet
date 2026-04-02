package stratum

import (
	"fmt"
	"net"
	"testing"

	testingtools "github.com/block/proto-fleet/server/internal/infrastructure/stratum/v1/testing"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
)

func NewSecret(s string) *secrets.Text {
	sec := secrets.NewText(s)
	return sec
}
func TestStratumConnect(t *testing.T) {
	inputs := []struct {
		Name     string
		Username string
		Password *secrets.Text
		Expected bool
		Err      error
	}{
		{
			Name:     "password no worker expect true",
			Username: "proto_mining_sw_test",
			Password: NewSecret("anything123"),
			Expected: true,
		},
		{
			Name:     "password with worker expect true",
			Username: "proto_mining_sw_test.012c",
			Password: NewSecret("anything1234"),
			Expected: true,
		},
		{
			Name:     "wrong user and password expect false",
			Username: "proto_mining_sw_test.012c",
			Password: NewSecret("anything1235"),
			Expected: false,
		},
		{
			Name:     "no user and no password expect false",
			Username: "proto_mining_sw_test.012c",
			Expected: false,
		},
		{
			Name:     "empty username expect false",
			Username: "",
			Password: NewSecret("anything123"),
			Expected: false,
		},
		{
			Name:     "empty password expect true",
			Username: "proto_mining_sw_test",
			Password: NewSecret(""),
			Expected: true,
		},
		{
			Name:     "nil password expect true",
			Username: "proto_mining_sw_test",
			Expected: true,
		},
		{
			Name:     "nil password,return err expect false and err",
			Username: "proto_mining_sw_test",
			Expected: false,
			Err:      fmt.Errorf("random error"),
		},
		{
			Name:     "password,return err expect false and err",
			Username: "proto_mining_sw_test",
			Password: NewSecret("anything123"),
			Expected: false,
			Err:      fmt.Errorf("random error"),
		},
	}

	for _, input := range inputs {
		t.Run(input.Name, func(t *testing.T) {
			fakeSVC := testingtools.NewFakeStratumService()
			require.NotNil(t, fakeSVC)

			fakeSVC.EXPECT().
				Authorize(input.Username, input.Password).
				Return(input.Expected, input.Err).
				Times(1)

			// We could use unix sockets, but we need to use TCP sockets for the test
			// to work on most operating systems.
			//nolint:gosec // For this test we want to bind to any available port.
			listener, err := net.Listen("tcp", ":0")
			require.NoError(t, err)
			//nolint:forcetypeassert // We hard code it to TCP and validated there was no error.
			listenerPort := listener.Addr().(*net.TCPAddr).Port

			closedConn := make(chan struct{})
			defer func() {
				close(closedConn)
			}()

			go func(t *testing.T) {
				conn, err := listener.Accept()
				assert.NoError(t, err)
				rpcConn := jsonrpc2.NewConn(t.Context(), jsonrpc2.NewPlainObjectStream(conn), fakeSVC)
				defer rpcConn.Close()
				<-closedConn
			}(t)

			result, err := Authenticate(t.Context(), fmt.Sprint("tcp://localhost:", listenerPort), input.Username, input.Password)
			if input.Err != nil {
				require.Error(t, err)
				// Not checking the jsonrpc2 error codes at this time.
				// assert.Equal(t, input.Err.Error(), err.Error())
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, input.Expected, result)
			assert.NoError(t, fakeSVC.ValidateExpectations())
		})
	}
}

func TestStratumConnectWithInvalidURL(t *testing.T) {
	inputs := []struct {
		Name     string
		URL      string
		Username string
		Password *secrets.Text
	}{
		{
			Name:     "invalid url",
			URL:      "invalid-url",
			Username: "proto_mining_sw_test",
			Password: NewSecret("anything123"),
		},
	}

	for _, input := range inputs {
		t.Run(input.Name, func(t *testing.T) {
			result, err := Authenticate(t.Context(), input.URL, input.Username, input.Password)
			require.Error(t, err)
			require.False(t, result)
		})
	}
}

// TODO: remove real endpoint from unit testing
func TestRealEndpoint(t *testing.T) {
	t.Skip("Skipping real endpoint test, only for manual execution")
	inputs := []struct {
		Name     string
		Username string
		Password *secrets.Text
		Expected bool
		URL      string
	}{
		{
			Name:     "password no worker expect true",
			Username: "proto_mining_sw_test",
			Password: NewSecret("anything123"),
			Expected: true,
			URL:      "stratum+tcp://stratum.braiins.com:3333",
		},
		{
			Name:     "password with worker expect true",
			Username: "proto_mining_sw_test.012c",
			Password: NewSecret("anything123"),
			Expected: true,
			URL:      "stratum+tcp://stratum.braiins.com:3333",
		},
		{
			Name:     "no password with worker expect true",
			Username: "proto_mining_sw_test.012c",
			Expected: true,
			URL:      "stratum+tcp://stratum.braiins.com:3333",
		},
		{
			Name:     "no password no username expect false",
			Username: "",
			Expected: false,
			URL:      "stratum+tcp://stratum.braiins.com:3333",
		},
	}

	for _, input := range inputs {
		t.Run(input.Name, func(t *testing.T) {
			ok, err := Authenticate(t.Context(), input.URL, input.Username, input.Password)
			require.NoError(t, err)
			assert.Equal(t, input.Expected, ok)
		})
	}
}
