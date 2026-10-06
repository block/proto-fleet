package marketdata

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// Opt in explicitly; normal tests never depend on internet access or live values.
func TestHTTPProviderLive(t *testing.T) {
	if os.Getenv("MARKET_DATA_LIVE_TEST") != "1" {
		t.Skip("set MARKET_DATA_LIVE_TEST=1 to verify public provider contracts")
	}
	config := testConfig()
	config.CoinGeckoAPIKey = os.Getenv("MARKET_DATA_COINGECKO_API_KEY")
	require.NoError(t, config.Validate(), "the live test requires a CoinGecko paid-plan API key")
	result, err := NewHTTPProvider(config).Fetch(t.Context())
	require.NoError(t, err)
	require.NotNil(t, result.Price)
	require.NotNil(t, result.Hashrate)
	require.NotNil(t, result.Hashprice)
	require.True(t, positiveFinite(result.Price.Value))
	require.True(t, positiveFinite(result.Hashrate.Value))
	require.True(t, positiveFinite(result.Hashprice.Value))
}
