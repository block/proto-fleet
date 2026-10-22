package hostedaccounts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInvitationCodeNormalization(t *testing.T) {
	want, err := invitationHash("K7MP-4X9R")
	require.NoError(t, err)
	for _, code := range []string{"K7MP4X9R", "k7mp-4x9r", " k7 mp - 4x 9r ", "k7mp 4x9r", strings.Repeat(" ", 35) + "K7MP4X9R"} {
		got, err := invitationHash(code)
		require.NoError(t, err, code)
		require.Equal(t, want, got, code)
	}
	for _, code := range []string{"", "K7MP", "K7MP4X9", "K7MP4X9RR", "K7MP-4X9R-6TWC-8NQF",
		"K7M-P4X9R", "K7MP--4X9R", "-K7MP4X9R", "K7MP4X9R-",
		"K7MP_4X9R", "K7MP\t4X9R", "K7MP/4X9R", "K7MP\u00a04X9R",
		"O7MP4X9R", "07MP4X9R", "I7MP4X9R", "17MP4X9R", strings.Repeat(" ", 129)} {
		_, err := invitationHash(code)
		require.Error(t, err, code)
	}
}

func TestInvitationCodesAreEightCharactersFromTheAlphabet(t *testing.T) {
	for range 50 {
		code, err := newInvitationCode()
		require.NoError(t, err)
		require.Regexp(t, `^[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}$`, code)
	}
}
