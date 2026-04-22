package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResultType_Valid(t *testing.T) {
	cases := map[ResultType]bool{
		ResultSuccess:              true,
		ResultFailure:              true,
		ResultUnknown:              true,
		"":                         false,
		ResultType("SUCCESS"):      false, // case-sensitive
		ResultType("not-a-result"): false,
	}
	for r, want := range cases {
		t.Run(string(r), func(t *testing.T) {
			assert.Equal(t, want, r.Valid())
		})
	}
}
