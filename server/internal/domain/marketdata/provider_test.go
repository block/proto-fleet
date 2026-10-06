package marketdata

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/transportguard"
	"github.com/stretchr/testify/require"
)

const (
	pricePath     = "/v2/prices/BTC-USD/spot"
	coinGeckoPath = "/api/v3/simple/price"
	networkPath   = "/api/v1/mining/hashrate/3d"
	rewardsPath   = "/api/v1/mining/reward-stats/144"
	priceJSON     = `{"data":{"amount":"100000","base":"BTC","currency":"USD"}}`
	coinGeckoJSON = `{"bitcoin":{"usd":100000}}`
	networkJSON   = `{"currentHashrate":1e21,"currentDifficulty":1e14}`
	rewardsJSON   = `{"startBlock":900000,"endBlock":900143,"totalReward":"46800000000"}`
)

func TestHTTPProviderCoinbase(t *testing.T) {
	for _, tt := range []struct {
		name      string
		path      string
		body      string
		status    int
		price     bool
		hashrate  bool
		hashprice bool
		wantError bool
	}{
		{name: "complete", price: true, hashrate: true, hashprice: true},
		{name: "numeric rewards", path: rewardsPath, body: strings.ReplaceAll(rewardsJSON, `"46800000000"`, `46800000000`), price: true, hashrate: true, hashprice: true},
		{name: "rate limited price", path: pricePath, status: 429, hashrate: true, wantError: true},
		{name: "network outage", path: networkPath, status: 503, price: true, wantError: true},
		{name: "rewards outage", path: rewardsPath, status: 503, price: true, hashrate: true, wantError: true},
		{name: "malformed JSON", path: pricePath, body: `{"data":`, hashrate: true, wantError: true},
		{name: "HTML response", path: networkPath, body: `<html>proxy error</html>`, price: true, wantError: true},
		{name: "negative price", path: pricePath, body: strings.ReplaceAll(priceJSON, `100000`, `-1`), hashrate: true, wantError: true},
		{name: "infinite price", path: pricePath, body: strings.ReplaceAll(priceJSON, `100000`, `Inf`), hashrate: true, wantError: true},
		{name: "NaN price", path: pricePath, body: strings.ReplaceAll(priceJSON, `100000`, `NaN`), hashrate: true, wantError: true},
		{name: "wrong currency", path: pricePath, body: strings.ReplaceAll(priceJSON, `USD`, `EUR`), hashrate: true, wantError: true},
		{name: "wrong asset", path: pricePath, body: strings.ReplaceAll(priceJSON, `BTC`, `ETH`), hashrate: true, wantError: true},
		{name: "missing price", path: pricePath, body: `{}`, hashrate: true, wantError: true},
		{name: "zero hashrate", path: networkPath, body: `{"currentHashrate":0,"currentDifficulty":1e14}`, price: true, hashprice: true, wantError: true},
		{name: "zero difficulty", path: networkPath, body: `{"currentHashrate":1e21,"currentDifficulty":0}`, price: true, hashrate: true, wantError: true},
		{name: "missing rewards", path: rewardsPath, body: `{}`, price: true, hashrate: true, wantError: true},
		{name: "zero reward", path: rewardsPath, body: strings.ReplaceAll(rewardsJSON, `46800000000`, `0`), price: true, hashrate: true, wantError: true},
		{name: "wrong sample size", path: rewardsPath, body: strings.ReplaceAll(rewardsJSON, `900143`, `900142`), price: true, hashrate: true, wantError: true},
		{name: "reversed sample", path: rewardsPath, body: strings.ReplaceAll(rewardsJSON, `900143`, `899999`), price: true, hashrate: true, wantError: true},
		{name: "reversed sample with overflow", path: rewardsPath, body: fmt.Sprintf(`{"startBlock":%d,"endBlock":%d,"totalReward":"46800000000"}`, int64(math.MaxInt64), int64(math.MinInt64+142)), price: true, hashrate: true, wantError: true},
		{name: "sample near maximum height", path: rewardsPath, body: fmt.Sprintf(`{"startBlock":%d,"endBlock":%d,"totalReward":"46800000000"}`, int64(math.MaxInt64-143), int64(math.MaxInt64)), price: true, hashrate: true, hashprice: true},
		{name: "oversized body", path: pricePath, body: strings.Repeat(" ", maxBodyBytes+1), hashrate: true, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "application/json", r.Header.Get("Accept"))
				require.Empty(t, r.Header.Get("Cookie"))
				require.Empty(t, r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("X-Cg-Pro-Api-Key"))
				if tt.path == r.URL.Path {
					if tt.status != 0 {
						w.WriteHeader(tt.status)
						return
					}
					_, _ = fmt.Fprint(w, tt.body)
					return
				}
				fixtures := map[string]string{pricePath: priceJSON, networkPath: networkJSON, rewardsPath: rewardsJSON}
				body, ok := fixtures[r.URL.Path]
				if !ok {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				_, _ = fmt.Fprint(w, body)
			}))
			defer server.Close()
			provider := NewHTTPProvider(Config{PriceProvider: PriceProviderCoinbase, CoinbaseURL: server.URL, CoinGeckoAPIKey: "test-key", MempoolURL: server.URL})
			at := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
			provider.now = func() time.Time { return at }
			result, err := provider.Fetch(t.Context())
			require.Equal(t, tt.wantError, err != nil)
			if tt.path == networkPath && tt.status != 0 {
				require.NotContains(t, err.Error(), "invalid difficulty")
			}
			require.Equal(t, tt.price, result.Price != nil)
			require.Equal(t, tt.hashrate, result.Hashrate != nil)
			require.Equal(t, tt.hashprice, result.Hashprice != nil)
			if result.Price != nil {
				require.Equal(t, 100000.0, result.Price.Value)
				require.Equal(t, at, result.Price.RetrievedAt)
			}
			if result.Hashrate != nil {
				require.Equal(t, 1e21, result.Hashrate.Value)
			}
			if result.Hashprice != nil {
				// Mean reward is 3.25 BTC, including fees (not just 3.125 subsidy).
				require.InDelta(t, 65.37884473800659, result.Hashprice.Value, 0.000001)
				require.Equal(t, at, result.Hashprice.RetrievedAt)
			}
		})
	}
}

func TestHTTPProviderCoinGeckoPriceProvider(t *testing.T) {
	for _, tt := range []struct {
		name   string
		body   string
		status int
	}{
		{name: "complete", body: coinGeckoJSON},
		{name: "unauthorized", status: http.StatusUnauthorized},
		{name: "forbidden", status: http.StatusForbidden},
		{name: "rate limited", status: http.StatusTooManyRequests},
		{name: "upstream outage", status: http.StatusServiceUnavailable},
		{name: "missing price", body: `{}`},
		{name: "zero price", body: `{"bitcoin":{"usd":0}}`},
		{name: "negative price", body: `{"bitcoin":{"usd":-1}}`},
		{name: "malformed JSON", body: `{"bitcoin":`},
		{name: "wrong type", body: `{"bitcoin":{"usd":"100000"}}`},
		{name: "oversized body", body: strings.Repeat(" ", maxBodyBytes+1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "application/json", r.Header.Get("Accept"))
				require.Empty(t, r.Header.Get("Cookie"))
				require.Empty(t, r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("X-Cg-Demo-Api-Key"))
				require.NotContains(t, r.URL.String(), "test-key")
				switch r.URL.Path {
				case coinGeckoPath:
					require.Equal(t, "test-key", r.Header.Get("X-Cg-Pro-Api-Key"))
					require.Equal(t, "ids=bitcoin&vs_currencies=usd", r.URL.RawQuery)
					if tt.status != 0 {
						w.WriteHeader(tt.status)
						_, _ = fmt.Fprint(w, "test-key") // response bodies must not leak into errors
						return
					}
					_, _ = fmt.Fprint(w, tt.body)
				case networkPath:
					require.Empty(t, r.Header.Get("X-Cg-Pro-Api-Key"))
					_, _ = fmt.Fprint(w, networkJSON)
				case rewardsPath:
					require.Empty(t, r.Header.Get("X-Cg-Pro-Api-Key"))
					_, _ = fmt.Fprint(w, rewardsJSON)
				default:
					t.Errorf("unexpected request to %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			// Omitted provider selection must use CoinGecko, never fall back to Coinbase.
			provider := NewHTTPProvider(Config{CoinbaseURL: server.URL, CoinGeckoURL: server.URL, CoinGeckoAPIKey: "test-key", MempoolURL: server.URL})
			provider.client.Transport = server.Client().Transport
			at := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
			provider.now = func() time.Time { return at }

			result, err := provider.Fetch(t.Context())
			require.NotNil(t, result.Hashrate)
			if tt.name != "complete" {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "test-key")
				require.Nil(t, result.Price)
				require.Nil(t, result.Hashprice)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result.Price)
			require.Equal(t, 100000.0, result.Price.Value)
			require.Equal(t, at, result.Price.RetrievedAt)
			require.Equal(t, "CoinGecko", result.Price.Source)
			require.NotNil(t, result.Hashprice)
			require.Equal(t, "CoinGecko + mempool.space", result.Hashprice.Source)
			require.InDelta(t, 65.37884473800659, result.Hashprice.Value, 0.000001)
		})
	}
}

func TestCoinGeckoProviderRejectsMissingKeyBeforeRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	provider := NewHTTPProvider(Config{CoinGeckoURL: server.URL})
	_, err := provider.priceProvider.FetchBitcoinPriceUSD(t.Context())
	require.ErrorContains(t, err, "MARKET_DATA_COINGECKO_API_KEY")
	require.Zero(t, calls.Load())
}

func TestHTTPProviderRejectsRedirects(t *testing.T) {
	for _, status := range []int{
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var redirectedCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				redirectedCalls.Add(1)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer target.Close()
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == coinGeckoPath {
					require.Equal(t, "test-key", r.Header.Get("X-Cg-Pro-Api-Key"))
				}
				http.Redirect(w, r, target.URL, status)
			}))
			defer upstream.Close()
			provider := NewHTTPProvider(Config{CoinGeckoURL: upstream.URL, CoinGeckoAPIKey: "test-key", MempoolURL: upstream.URL})
			provider.client.Transport = upstream.Client().Transport
			result, err := provider.Fetch(t.Context())
			require.ErrorIs(t, err, transportguard.ErrRedirectNotAllowed)
			require.Zero(t, redirectedCalls.Load(), "redirect destinations must never be contacted")
			require.Nil(t, result.Price)
			require.Nil(t, result.Hashrate)
			require.Nil(t, result.Hashprice)
		})
	}
}

func TestHTTPProviderTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	provider := NewHTTPProvider(Config{PriceProvider: PriceProviderCoinbase, CoinbaseURL: server.URL, MempoolURL: server.URL})
	provider.client.Timeout = 20 * time.Millisecond
	result, err := provider.Fetch(t.Context())
	require.Error(t, err)
	require.Nil(t, result.Price)
	require.Nil(t, result.Hashrate)
	require.Nil(t, result.Hashprice)
}
