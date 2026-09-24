package containers

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContainerExternalURL(t *testing.T) {
	for _, tc := range []struct{ scheme, ip, port, want string }{
		{"http", "192.168.1.20", "8080", "http://192.168.1.20:8080"},
		{"https", "fd00::20", "443", "https://[fd00::20]:443"},
		{"javascript", "192.168.1.20", "80", ""},
		{"http", "192.168.1.20", "65536", ""},
		{"http", "192.168.1.20", "-1", ""},
		{"http", "", "80", ""},
		{"http", "example.com@evil.test", "80", ""},
	} {
		require.Equal(t, tc.want, webViewURL(tc.scheme, tc.ip, tc.port))
	}
}
