// Package marketdata retrieves and caches public Bitcoin market data.
package marketdata

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

type PriceProviderName string

const (
	PriceProviderCoinbase  PriceProviderName = "coinbase"
	PriceProviderCoinGecko PriceProviderName = "coingecko"
)

type Config struct {
	Enabled         bool              `help:"Enable dashboard market data and outbound public feed requests" default:"false" env:"ENABLED"`
	RefreshInterval time.Duration     `help:"Minimum interval between market-data refreshes" default:"1m" env:"REFRESH_INTERVAL"`
	PriceProvider   PriceProviderName `help:"Bitcoin USD price provider" default:"coingecko" enum:"coinbase,coingecko" env:"PRICE_PROVIDER"`
	CoinbaseURL     string            `help:"Coinbase API base URL" default:"https://api.coinbase.com" env:"COINBASE_URL"`
	CoinGeckoURL    string            `name:"coingecko-url" help:"CoinGecko Pro API base URL" default:"https://pro-api.coingecko.com" env:"COINGECKO_URL"`
	CoinGeckoAPIKey string            `name:"coingecko-api-key" help:"CoinGecko paid-plan API key for the Pro API (prefer the environment variable)" env:"COINGECKO_API_KEY" json:"-"`
	MempoolURL      string            `help:"Mempool API base URL (a self-hosted mainnet instance may be used)" default:"https://mempool.space" env:"MEMPOOL_URL"`
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.RefreshInterval < time.Minute || c.RefreshInterval > 15*time.Minute {
		return fmt.Errorf("market-data refresh interval must be between 1m and 15m")
	}
	priceProvider := c.effectivePriceProvider()
	if !priceProvider.valid() {
		return fmt.Errorf("market-data price provider must be one of %q or %q", PriceProviderCoinbase, PriceProviderCoinGecko)
	}
	for _, raw := range []string{c.priceProviderURL(priceProvider), c.MempoolURL} {
		if err := validateBaseURL(raw); err != nil {
			return err
		}
	}
	if priceProvider == PriceProviderCoinGecko {
		if err := validateCoinGeckoAPIKey(c.CoinGeckoAPIKey); err != nil {
			return err
		}
		if !strings.HasPrefix(c.CoinGeckoURL, "https://") {
			return fmt.Errorf("MARKET_DATA_COINGECKO_URL must use HTTPS")
		}
	}
	return nil
}

func (c Config) effectivePriceProvider() PriceProviderName {
	if c.PriceProvider == "" {
		return PriceProviderCoinGecko
	}
	return c.PriceProvider
}

func validateCoinGeckoAPIKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("MARKET_DATA_COINGECKO_API_KEY requires a CoinGecko paid-plan API key when CoinGecko is enabled")
	}
	if strings.ContainsAny(key, "\r\n") {
		return fmt.Errorf("MARKET_DATA_COINGECKO_API_KEY must not contain line breaks")
	}
	return nil
}

func (c Config) priceProviderURL(provider PriceProviderName) string {
	switch provider {
	case PriceProviderCoinbase:
		return c.CoinbaseURL
	case PriceProviderCoinGecko:
		return c.CoinGeckoURL
	default:
		return ""
	}
}

func (p PriceProviderName) valid() bool {
	switch p {
	case PriceProviderCoinbase, PriceProviderCoinGecko:
		return true
	default:
		return false
	}
}

func validateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("market-data base URLs must be HTTP(S) URLs without credentials, query, or fragment")
	}
	return nil
}
