package marketdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/transportguard"
)

const (
	requestTimeout = 8 * time.Second
	rewardBlocks   = 144
	maxBodyBytes   = 256 * 1024
)

// Metric uses the units documented on Snapshot. Samples are immutable in the cache.
type Metric struct {
	Value       float64
	RetrievedAt time.Time
	Source      string
	Stale       bool
}

type Snapshot struct {
	RefreshIntervalSeconds uint32
	Enabled                bool
	Price                  *Metric // USD/BTC
	Hashprice              *Metric // USD/PH/day, estimated gross revenue
	Hashrate               *Metric // H/s, latest 1008-block estimate
}

// Provider may return partial data alongside an error. Missing values must not
// overwrite last-known-good samples; hashprice must use one complete input set.
type Provider interface {
	Fetch(ctx context.Context) (Snapshot, error)
}

type HTTPProvider struct {
	client      *http.Client
	coinbaseURL string
	mempoolURL  string
	now         func() time.Time
}

func NewHTTPProvider(config Config) *HTTPProvider {
	return &HTTPProvider{
		client:      &http.Client{Timeout: requestTimeout, CheckRedirect: transportguard.RejectRedirect},
		coinbaseURL: strings.TrimRight(config.CoinbaseURL, "/"),
		mempoolURL:  strings.TrimRight(config.MempoolURL, "/"),
		now:         time.Now,
	}
}

func (p *HTTPProvider) get(ctx context.Context, endpoint string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create market-data request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ProtoFleet/market-data")
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("request market data: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("market-data upstream HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return fmt.Errorf("read market data: %w", err)
	}
	if len(body) > maxBodyBytes {
		return fmt.Errorf("market-data response exceeds size limit")
	}
	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("decode market data: %w", err)
	}
	return nil
}

func positiveFinite(v float64) bool {
	return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
}

func (p *HTTPProvider) Fetch(ctx context.Context) (Snapshot, error) {
	var price struct {
		Data struct {
			Amount   string `json:"amount"`
			Base     string `json:"base"`
			Currency string `json:"currency"`
		} `json:"data"`
	}
	var network struct {
		Hashrate   float64 `json:"currentHashrate"`
		Difficulty float64 `json:"currentDifficulty"`
	}
	var rewards struct {
		Start       int64       `json:"startBlock"`
		End         int64       `json:"endBlock"`
		TotalReward json.Number `json:"totalReward"` // satoshis, quoted on mempool.space
	}
	var priceErr, networkErr, rewardErr error
	var priceAt, networkAt, rewardAt time.Time
	var wg sync.WaitGroup
	wg.Go(func() {
		priceErr = p.get(ctx, p.coinbaseURL+"/v2/prices/BTC-USD/spot", &price)
		priceAt = p.now().UTC()
	})
	wg.Go(func() {
		networkErr = p.get(ctx, p.mempoolURL+"/api/v1/mining/hashrate/3d", &network)
		networkAt = p.now().UTC()
	})
	wg.Go(func() {
		rewardErr = p.get(ctx, p.mempoolURL+"/api/v1/mining/reward-stats/144", &rewards)
		rewardAt = p.now().UTC()
	})
	wg.Wait()

	result := Snapshot{Enabled: true}
	usd, parseErr := strconv.ParseFloat(price.Data.Amount, 64)
	if priceErr == nil {
		if parseErr != nil || !positiveFinite(usd) || price.Data.Currency != "USD" || price.Data.Base != "BTC" {
			priceErr = fmt.Errorf("price feed returned an invalid USD price")
		} else {
			result.Price = &Metric{Value: usd, RetrievedAt: priceAt, Source: "Coinbase"}
		}
	}
	networkFetched := networkErr == nil
	if networkFetched && positiveFinite(network.Hashrate) {
		result.Hashrate = &Metric{Value: network.Hashrate, RetrievedAt: networkAt, Source: "mempool.space"}
	} else if networkErr == nil {
		networkErr = fmt.Errorf("network feed returned an invalid hashrate")
	}
	// An unavailable hashrate doesn't invalidate difficulty, and vice versa.
	// Hashprice depends on difficulty, not the noisy network-hashrate estimate.
	totalReward, parseErr := rewards.TotalReward.Float64()
	if rewardErr == nil && (parseErr != nil || !positiveFinite(totalReward) || rewards.Start < 0 || rewards.End-rewards.Start != rewardBlocks-1) {
		rewardErr = fmt.Errorf("reward feed returned an invalid 144-block sample")
	}
	var difficultyErr error
	if networkFetched && !positiveFinite(network.Difficulty) {
		difficultyErr = fmt.Errorf("network feed returned an invalid difficulty")
	}
	if result.Price != nil && networkFetched && difficultyErr == nil && rewardErr == nil {
		// Expected daily hashes for 1 PH/s divided by expected hashes/block,
		// multiplied by mean gross block reward (subsidy + fees) and USD/BTC.
		value := 1e15 * 86400 / (network.Difficulty * (1 << 32)) * (totalReward / rewardBlocks / 1e8) * usd
		if positiveFinite(value) {
			result.Hashprice = &Metric{Value: value, RetrievedAt: minTime(priceAt, networkAt, rewardAt), Source: "Coinbase + mempool.space"}
		} else {
			rewardErr = fmt.Errorf("hashprice calculation returned an invalid value")
		}
	}
	return result, errors.Join(priceErr, networkErr, rewardErr, difficultyErr)
}

func minTime(times ...time.Time) time.Time {
	result := times[0]
	for _, t := range times[1:] {
		if t.Before(result) {
			result = t
		}
	}
	return result
}
