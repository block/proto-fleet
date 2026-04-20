package workername

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFromPoolUsername(t *testing.T) {
	t.Run("returns suffix after first dot", func(t *testing.T) {
		assert.Equal(t, "worker-01", FromPoolUsername("wallet.worker-01"))
	})

	t.Run("preserves dots inside worker name", func(t *testing.T) {
		assert.Equal(t, "main.worker-01", FromPoolUsername("wallet.main.worker-01"))
	})

	t.Run("returns empty when username has no valid separator", func(t *testing.T) {
		assert.Empty(t, FromPoolUsername("wallet"))
		assert.Empty(t, FromPoolUsername(".worker-01"))
		assert.Empty(t, FromPoolUsername("wallet."))
	})
}
