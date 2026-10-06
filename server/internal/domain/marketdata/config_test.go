package marketdata

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCoinGeckoConfig(t *testing.T) {
	for _, tt := range []struct {
		name      string
		provider  PriceProviderName
		key       string
		url       string
		disabled  bool
		wantError string
	}{
		{name: "Pro key", provider: PriceProviderCoinGecko, key: "test-key"},
		{name: "default selects CoinGecko", key: "test-key"},
		{name: "missing key", wantError: "MARKET_DATA_COINGECKO_API_KEY"},
		{name: "whitespace key", key: " \t ", wantError: "MARKET_DATA_COINGECKO_API_KEY"},
		{name: "line breaks", key: "test-key\r\ninvalid", wantError: "MARKET_DATA_COINGECKO_API_KEY"},
		{name: "HTTPS required", key: "test-key", url: "http://price.example.invalid", wantError: "must use HTTPS"},
		{name: "compatible HTTPS origin", key: "test-key", url: "https://price.example.invalid/api-proxy"},
		{name: "disabled needs no key", disabled: true},
		{name: "explicit Coinbase needs no key", provider: PriceProviderCoinbase, url: "not a URL"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := testConfig()
			config.PriceProvider = tt.provider
			config.CoinGeckoAPIKey = tt.key
			config.Enabled = !tt.disabled
			if tt.url != "" {
				config.CoinGeckoURL = tt.url
			}
			err := config.Validate()
			if tt.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantError)
			if tt.key != "" {
				require.NotContains(t, err.Error(), tt.key)
			}
		})
	}
}

func TestCoinGeckoAPIKeyIsNotSerialized(t *testing.T) {
	encoded, err := json.Marshal(testConfig())
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "test-key")
	require.NotContains(t, string(encoded), "CoinGeckoAPIKey")
}
