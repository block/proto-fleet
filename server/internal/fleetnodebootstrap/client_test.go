package fleetnodebootstrap

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
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
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", "http://attacker.example.com/")
				w.WriteHeader(code)
			})
			srv := httptest.NewUnstartedServer(h2c.NewHandler(handler, &http2.Server{}))
			srv.Start()
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
