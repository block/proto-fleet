package workername

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestEffectivePoolUsername(t *testing.T) {
	require.Equal(t, "wallet.rig-1", EffectivePoolUsername(" wallet ", "rig-1", true))
	require.Equal(t, "wallet.existing", EffectivePoolUsername("wallet.existing", "rig-1", true))
	require.Equal(t, "wallet", EffectivePoolUsername(" wallet ", "rig-1", false))
	require.Equal(t, "wallet.rig-2", RewritePoolUsername("wallet.old", "rig-2"))
}
