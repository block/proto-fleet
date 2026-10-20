package hostedaccounts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInvitationCodeNormalization(t *testing.T) {
	want, err := invitationHash("K7MP-4X9R-6TWC-8NQF")
	require.NoError(t, err)
	for _, code := range []string{"K7MP4X9R6TWC8NQF", "k7mp-4x9r-6twc-8nqf", " k7 mp - 4x 9r - 6twc - 8nqf ", "k7mp 4x9r 6twc 8nqf", "K7MP4X9R-6TWC8NQF", strings.Repeat(" ", 35) + "K7MP4X9R6TWC8NQF"} {
		got, err := invitationHash(code)
		require.NoError(t, err, code)
		require.Equal(t, want, got, code)
	}
	for _, code := range []string{"", "K7MP-4X9R", "K7MP4X9R", "K7MP4X9R6TWC8NQ", "K7MP4X9R6TWC8NQFF",
		"K7M-P4X9R6TWC8NQF", "K7MP--4X9R-6TWC-8NQF", "-K7MP4X9R6TWC8NQF", "K7MP4X9R6TWC8NQF-",
		"K7MP_4X9R6TWC8NQF", "K7MP\t4X9R6TWC8NQF", "K7MP/4X9R6TWC8NQF",
		"O7MP4X9R6TWC8NQF", "07MP4X9R6TWC8NQF", "I7MP4X9R6TWC8NQF", "17MP4X9R6TWC8NQF", "K7MP4X9R6TWC8NQF", strings.Repeat(" ", 129)} {
		_, err := invitationHash(code)
		require.Error(t, err, code)
	}
}
