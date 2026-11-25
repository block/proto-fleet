package frigateupdate

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func TestTruncateKeepsValidUTF8AtEveryLimit(t *testing.T) {
	for _, r := range []string{"é", "€", "🚀"} {
		for pad := range utf8.RuneLen([]rune(r)[0]) + 1 {
			for _, limit := range []int{128, maxStoredMessage} {
				in := strings.Repeat("a", limit-pad) + r + "tail"
				got := truncate(in, limit)
				assert.True(t, utf8.ValidString(got), "rune %q pad %d limit %d", r, pad, limit)
				assert.LessOrEqual(t, len(got), limit)
				assert.True(t, strings.HasPrefix(in, got))
				assert.GreaterOrEqual(t, len(got), limit-utf8.UTFMax+1, "cut no further back than one rune")
			}
		}
	}
	assert.Equal(t, "short", truncate("short", 128))
	assert.Equal(t, "ab", truncate("a\xffb", 128), "invalid bytes are dropped")
	assert.Empty(t, truncate("🚀", 2))
}
