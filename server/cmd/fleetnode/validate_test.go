package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnrollCmd_SurfacesValidateServerURLErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		url           string
		allowInsecure bool
		wantSub       string
	}{
		{name: "remote http rejected", url: "http://fleet.example.com", allowInsecure: false, wantSub: "https"},
		{name: "unknown scheme rejected", url: "ftp://fleet.example.com", allowInsecure: false, wantSub: "scheme"},
		{name: "missing host rejected", url: "https://", allowInsecure: false, wantSub: "host"},
		{name: "userinfo rejected", url: "https://user:pass@fleet.example.com", allowInsecure: false, wantSub: "userinfo"},
		{name: "query rejected", url: "https://fleet.example.com?foo=bar", allowInsecure: false, wantSub: "query"},
		{name: "fragment rejected", url: "https://fleet.example.com#frag", allowInsecure: false, wantSub: "fragment"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Arrange
			dir := t.TempDir()
			cmd := &EnrollCmd{
				ServerURL:              tc.url,
				Name:                   "node-x",
				AllowInsecureTransport: tc.allowInsecure,
			}

			// Act
			err := cmd.run(&Context{StateDir: dir}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})

			// Assert
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantSub)
		})
	}
}
