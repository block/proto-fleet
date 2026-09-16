package sqlstores

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLikeSearchPattern(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  sql.NullString
	}{
		{"wraps and escapes wildcards", `  100% up_time \ ok  `, sql.NullString{String: `%100\% up\_time \\ ok%`, Valid: true}},
		{"empty is no search", "", sql.NullString{}},
		// Whitespace must not become a "% %" pattern that matches every row
		// containing a space.
		{"blank is no search", " \t\n", sql.NullString{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, likeSearchPattern(tc.query))
		})
	}
}
