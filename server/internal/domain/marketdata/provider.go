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

type BitcoinPriceProvider interface {
	FetchBitcoinPriceUSD(ctx context.Context) (*Metric, error)
}

type HTTPProvider struct {
	client        *http.Client
	priceProvider BitcoinPriceProvider
	mempoolURL    string
	now           func() time.Time
}

func NewHTTPProvider(config Config) *HTTPProvider {
	provider := &HTTPProvider{
		client:     &http.Client{Timeout: requestTimeout, CheckRedirect: transportguard.RejectRedirect},
		mempoolURL: strings.TrimRight(config.MempoolURL, "/"),
		now:        time.Now,
	}
	provider.priceProvider = newBitcoinPriceProvider(config, provider.client, provider.currentTime)
	return provider
}

func (p *HTTPProvider) currentTime() time.Time {
	return p.now().UTC()
}

func newBitcoinPriceProvider(config Config, client *http.Client, now func() time.Time) BitcoinPriceProvider {
	switch config.effectivePriceProvider() {
	case PriceProviderCoinGecko:
		return &CoinGeckoPriceProvider{client: client, baseURL: strings.TrimRight(config.CoinGeckoURL, "/"), apiKey: config.CoinGeckoAPIKey, now: now}
	case PriceProviderCoinbase:
		return &CoinbasePriceProvider{client: client, baseURL: strings.TrimRight(config.CoinbaseURL, "/"), now: now}
	default:
		return unsupportedPriceProvider(config.PriceProvider)
	}
}

func getJSON(ctx context.Context, client *http.Client, endpoint string, headers http.Header, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create market-data request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ProtoFleet/market-data")
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	resp, err := client.Do(req)
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

type CoinbasePriceProvider struct {
	client  *http.Client
	baseURL string
	now     func() time.Time
}

func (p *CoinbasePriceProvider) FetchBitcoinPriceUSD(ctx context.Context) (*Metric, error) {
	var price struct {
		Data struct {
			Amount   string `json:"amount"`
			Base     string `json:"base"`
			Currency string `json:"currency"`
		} `json:"data"`
	}
	if err := getJSON(ctx, p.client, p.baseURL+"/v2/prices/BTC-USD/spot", nil, &price); err != nil {
		return nil, err
	}
	at := p.now()
	usd, err := strconv.ParseFloat(price.Data.Amount, 64)
	if err != nil || !positiveFinite(usd) || price.Data.Currency != "USD" || price.Data.Base != "BTC" {
		return nil, fmt.Errorf("price feed returned an invalid USD price")
	}
	return &Metric{Value: usd, RetrievedAt: at, Source: "Coinbase"}, nil
}

type CoinGeckoPriceProvider struct {
	client  *http.Client
	baseURL string
	apiKey  string
	now     func() time.Time
}

func (p *CoinGeckoPriceProvider) FetchBitcoinPriceUSD(ctx context.Context) (*Metric, error) {
	if err := validateCoinGeckoAPIKey(p.apiKey); err != nil {
		return nil, err
	}
	var price struct {
		Bitcoin struct {
			USD float64 `json:"usd"`
		} `json:"bitcoin"`
	}
	headers := make(http.Header)
	headers.Set("X-Cg-Pro-Api-Key", p.apiKey)
	if err := getJSON(ctx, p.client, p.baseURL+"/api/v3/simple/price?ids=bitcoin&vs_currencies=usd", headers, &price); err != nil {
		return nil, err
	}
	at := p.now()
	if !positiveFinite(price.Bitcoin.USD) {
		return nil, fmt.Errorf("price feed returned an invalid USD price")
	}
	return &Metric{Value: price.Bitcoin.USD, RetrievedAt: at, Source: "CoinGecko"}, nil
}

type unsupportedPriceProvider PriceProviderName

func (p unsupportedPriceProvider) FetchBitcoinPriceUSD(context.Context) (*Metric, error) {
	return nil, fmt.Errorf("unsupported market-data price provider %q", string(p))
}

func positiveFinite(v float64) bool {
	return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
}

func (p *HTTPProvider) Fetch(ctx context.Context) (Snapshot, error) {
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
	var price *Metric
	var networkAt, rewardAt time.Time
	var wg sync.WaitGroup
	wg.Go(func() {
		price, priceErr = p.priceProvider.FetchBitcoinPriceUSD(ctx)
	})
	wg.Go(func() {
		networkErr = getJSON(ctx, p.client, p.mempoolURL+"/api/v1/mining/hashrate/3d", nil, &network)
		networkAt = p.currentTime()
	})
	wg.Go(func() {
		rewardErr = getJSON(ctx, p.client, p.mempoolURL+"/api/v1/mining/reward-stats/144", nil, &rewards)
		rewardAt = p.currentTime()
	})
	wg.Wait()

	result := Snapshot{Enabled: true, Price: price}
	networkFetched := networkErr == nil
	if networkFetched && positiveFinite(network.Hashrate) {
		result.Hashrate = &Metric{Value: network.Hashrate, RetrievedAt: networkAt, Source: "mempool.space"}
	} else if networkErr == nil {
		networkErr = fmt.Errorf("network feed returned an invalid hashrate")
	}
	// An unavailable hashrate doesn't invalidate difficulty, and vice versa.
	// Hashprice depends on difficulty, not the noisy network-hashrate estimate.
	totalReward, parseErr := rewards.TotalReward.Float64()
	if rewardErr == nil && (parseErr != nil || !positiveFinite(totalReward) || rewards.Start < 0 || rewards.End < rewards.Start || rewards.End-rewards.Start != rewardBlocks-1) {
		rewardErr = fmt.Errorf("reward feed returned an invalid 144-block sample")
	}
	var difficultyErr error
	if networkFetched && !positiveFinite(network.Difficulty) {
		difficultyErr = fmt.Errorf("network feed returned an invalid difficulty")
	}
	if result.Price != nil && networkFetched && difficultyErr == nil && rewardErr == nil {
		// Expected daily hashes for 1 PH/s divided by expected hashes/block,
		// multiplied by mean gross block reward (subsidy + fees) and USD/BTC.
		value := 1e15 * 86400 / (network.Difficulty * (1 << 32)) * (totalReward / rewardBlocks / 1e8) * result.Price.Value
		if positiveFinite(value) {
			result.Hashprice = &Metric{Value: value, RetrievedAt: minTime(result.Price.RetrievedAt, networkAt, rewardAt), Source: result.Price.Source + " + mempool.space"}
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
