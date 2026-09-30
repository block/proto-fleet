// Package marketdata retrieves and caches public Bitcoin market data.
package marketdata

import (
	"fmt"
	"net/url"
	"time"
)

type Config struct {
	Enabled         bool          `help:"Enable dashboard market data and outbound public feed requests" default:"false" env:"ENABLED"`
	RefreshInterval time.Duration `help:"Minimum interval between market-data refreshes" default:"1m" env:"REFRESH_INTERVAL"`
	CoinbaseURL     string        `help:"Coinbase API base URL" default:"https://api.coinbase.com" env:"COINBASE_URL"`
	MempoolURL      string        `help:"Mempool API base URL (a self-hosted mainnet instance may be used)" default:"https://mempool.space" env:"MEMPOOL_URL"`
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.RefreshInterval < time.Minute || c.RefreshInterval > 15*time.Minute {
		return fmt.Errorf("market-data refresh interval must be between 1m and 15m")
	}
	for _, raw := range []string{c.CoinbaseURL, c.MempoolURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("market-data base URLs must be HTTP(S) URLs without credentials, query, or fragment")
		}
	}
	return nil
}
