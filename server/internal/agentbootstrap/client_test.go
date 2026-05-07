package agentbootstrap

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayHTTPClient_RejectsRedirect(t *testing.T) {
	t.Parallel()

	cases := []int{
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	}
	for _, code := range cases {
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Parallel()

			// Arrange
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", "http://attacker.example.com/")
				w.WriteHeader(code)
			}))
			t.Cleanup(srv.Close)
			client := newGatewayHTTPClient()

			// Act
			resp, err := client.Post(srv.URL, "application/proto", strings.NewReader(""))

			// Assert
			require.Error(t, err)
			assert.Contains(t, err.Error(), "redirects are not allowed")
			if resp != nil {
				_ = resp.Body.Close()
			}
		})
	}
}

func TestGatewayHTTPClient_HasTimeout(t *testing.T) {
	t.Parallel()

	// Act
	client := newGatewayHTTPClient()

	// Assert
	assert.Equal(t, httpClientTimeout, client.Timeout)
}
