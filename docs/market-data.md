# Bitcoin market data

When enabled, the ProtoFleet dashboard displays bitcoin's USD spot price,
estimated USD hashprice per PH/s per day, and estimated Bitcoin network hashrate in EH/s.
Market data is global, independent of the selected site and telemetry duration.
It is available to authenticated users with `fleet:read` at any site or across
the organization.

The feature is **off by default**. `MARKET_DATA_ENABLED=true` enables both the
dashboard section and server-side feed requests without a client rebuild. The
default price provider requires an operator-supplied CoinGecko paid-plan API key.

## Sources and units

The provider adapters use these endpoints:

| Input                | Source        | Endpoint                                                                 | Units / interpretation                                                                              |
| -------------------- | ------------- | ------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------- |
| Spot price           | Coinbase      | `https://api.coinbase.com/v2/prices/BTC-USD/spot`                        | `data.amount`: string, USD per BTC; verify `base=BTC` and `currency=USD`                            |
| Spot price           | CoinGecko Pro | `https://pro-api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=usd` | `bitcoin.usd`: numeric USD per BTC                                                              |
| Network / difficulty | mempool.space | `https://mempool.space/api/v1/mining/hashrate/3d`                        | `currentHashrate`: H/s; `currentDifficulty`: dimensionless Bitcoin difficulty                       |
| Gross block rewards  | mempool.space | `https://mempool.space/api/v1/mining/reward-stats/144`                   | `totalReward`: satoshis including subsidy and transaction fees; inclusive `startBlock` / `endBlock` |

The CoinGecko Pro API is the default spot-price provider. It requires an API key
from a paid CoinGecko plan; the subscription does not have to be the tier named
Pro. Paid tiers, including Basic, use the same Pro API authentication.
The `coingecko` provider supports only this paid API; Demo keys and keyless public
API access are not supported.

Set `MARKET_DATA_COINGECKO_API_KEY` to your own paid-plan API key. It is sent only to the
configured CoinGecko origin in the `x-cg-pro-api-key` header, never in a query
string or to the browser, Coinbase, or Mempool. CoinGecko origins must use HTTPS.
There is no automatic fallback to another price provider on authentication or
feed failures.

Set `MARKET_DATA_PRICE_PROVIDER=coinbase` to explicitly select Coinbase instead;
that adapter does not require a CoinGecko key. Mining inputs always come from
Mempool-compatible APIs. Requests originate from fleetd, not each user's browser,
and contain no fleet, site, miner, or user information.
Normal server-side egress metadata (including the server's IP address) is visible
to the providers. No provider uptime, price-publication cadence, or redistribution
rights are assumed. Confirm applicable provider terms and capacity before a
large production rollout.

Each operator must supply their own authorized credentials and comply with their
provider's terms, including attribution and permitted uses of derived data.
This integration does not grant a data license or include provider credentials.
Do not commit keys, embed them in client builds or container images, or share
them between independent deployments without appropriate authorization.

Primary references:

- [Coinbase price API](https://docs.cdp.coinbase.com/coinbase-app/track-apis/prices)
- [CoinGecko simple price API](https://docs.coingecko.com/reference/simple-price)
- [CoinGecko API authentication](https://docs.coingecko.com/docs/setting-up-your-api-key)
- [mempool.space REST API](https://mempool.space/docs/api/rest)
- [Mempool mining routes](https://github.com/mempool/mempool/blob/master/backend/src/api/mining/mining-routes.ts)

The Mempool backend obtains `currentHashrate` from Bitcoin Core's
`getnetworkhashps(1008)`. This is an approximately seven-day, 1008-block estimate,
not an instantaneous measurement. The `3d` URL controls returned historical
series, not the averaging window of `currentHashrate`.

## Estimated hashprice

```text
mean_reward_btc = totalReward / 144 / 100_000_000
expected_blocks_per_ph_day = 10^15 * 86_400 / (currentDifficulty * 2^32)
estimated_hashprice_usd_per_ph_day = expected_blocks_per_ph_day * mean_reward_btc * spot_price_usd
```

This is expected **gross** daily mining revenue for one PH/s using current
difficulty and the mean coinbase reward over the latest 144 blocks. It includes
transaction fees, excludes pool fees, electricity and operating costs, and
smooths changing fee conditions (including rewards around a halving).
The UI deliberately calls it **Hashprice (estimate)**: it is not a licensed
Hashrate Index/Luxor index, a realized payout, or a profitability prediction.
If an exact external benchmark is required, integrate that provider's licensed
API and methodology rather than relabeling this estimate.

Hashprice is recomputed only when all three inputs succeed in the same refresh.
An incomplete refresh never mixes a new price with retained difficulty/rewards.

## Refresh, outages, and privacy

- `MarketDataService.GetMarketData` returns a process-wide, in-memory snapshot.
  Requests coalesce into at most one refresh every configured interval, even
  during upstream failures. Replicas have independent caches.
- The client checks feature availability once on dashboard mount. Until the
  server confirms enablement, the section is hidden. A disabled response stops
  polling and outbound feeds. An unanswered probe retries every minute while
  visible. Reload the page after enabling a previously disabled deployment.
- Permission denial hides the section and stops polling until it is remounted.
  The server evaluates `fleet:read` at any site; the client's org-only permission
  list cannot determine access for site-scoped readers.
- When enabled, the client checks every minute, pauses periodic requests in hidden
  tabs, and refreshes on return. Refresh requests do not overlap and abort on unmount.
- No outbound work occurs before an authenticated reader requests data. Each
  refresh makes three parallel HTTP requests with an eight-second deadline and
  bounded JSON bodies. Upstream redirects are rejected. Caller cancellation does
  not cancel a shared refresh.
- Each metric retains its last-known-good value independently. A refresh that
  cannot replace it marks it stale; values an hour old are omitted. Cold-start
  outages show “Unavailable”, never zero. Caches are not persisted across restarts.
- `retrieved_at` means successful retrieval, **not** provider publication time.
  The upstream responses do not supply reliable observation timestamps for all
  inputs. A responding but stale provider cannot be detected from retrieval time
  alone, so the UI notes that source data may be delayed.
- RPC/transport outages mark client-retained values stale too. The panel shows
  retrieval times, units, and stale indicators. Its info button opens the estimation
  method and delay caveats in a popover (a bottom sheet on phones). Source names
  remain in API metadata and this documentation, not in the dashboard UI.
- Browser retries use the same shared server cache and cannot force upstream
  requests faster than the configured minimum interval.

## Configuration

Configuration follows fleetd's Kong CLI / environment conventions:

| Environment variable           | Default                       | Purpose                                                                          |
| ------------------------------ | ----------------------------- | -------------------------------------------------------------------------------- |
| `MARKET_DATA_ENABLED`          | `false`                       | Opt in with `true` to show the dashboard section and allow public feed requests |
| `MARKET_DATA_REFRESH_INTERVAL` | `1m`                          | Minimum upstream interval; allowed range `1m`–`15m`                              |
| `MARKET_DATA_PRICE_PROVIDER`   | `coingecko`                   | BTC/USD provider: `coingecko` or `coinbase`                                      |
| `MARKET_DATA_COINBASE_URL`     | `https://api.coinbase.com`    | Coinbase-compatible API origin / optional base path                              |
| `MARKET_DATA_COINGECKO_URL`    | `https://pro-api.coingecko.com` | CoinGecko Pro-compatible HTTPS origin / optional base path                      |
| `MARKET_DATA_COINGECKO_API_KEY` | unset                        | Your CoinGecko paid-plan API key; required when enabled with CoinGecko selected |
| `MARKET_DATA_MEMPOOL_URL`      | `https://mempool.space`       | Mempool-compatible API origin / optional base path                               |

CLI equivalents are `--market-data-enabled=true`,
`--market-data-refresh-interval=5m`, `--market-data-price-provider=coingecko`,
`--market-data-coinbase-url=...`, `--market-data-coingecko-url=...`, and
`--market-data-mempool-url=...`. URLs must use HTTP(S) and cannot contain embedded
credentials, queries, or fragments. Use HTTPS for public providers.
Supply the API key through a protected environment/secret file rather than a
command-line argument that can appear in shell history or process listings.
Enabled CoinGecko configurations fail startup if the key is missing or the
CoinGecko origin is not HTTPS. Disabled deployments do not require a key.

HA installation captures these settings into the protected
`/etc/proto-fleet/ha/fleet.env` on both Fleet hosts; updates reuse that file.
See [HA market-data setup](../deployment-files/ha/README.md) for preserving the
settings through `sudo` during installation. In active/passive HA, the passive
host rejects market-data RPCs, so only the active host triggers feed refreshes.

A self-hosted Mempool **mainnet** instance must have mining indexing enabled and
serve both mining endpoints. Alternative origins must preserve the API semantics;
do not point them at testnet, signet, or regtest. Source labels name the upstream
API implementation, not the configured hostname.

No database migration or new dependency is needed.

### Enable in a dev deployment

After deploying a build containing this feature, set this in the deployment's
operator `.env`:

```sh
MARKET_DATA_ENABLED=true
MARKET_DATA_COINGECKO_API_KEY=your-pro-api-key
```

Replace the placeholder with your own CoinGecko paid-plan API key and protect the `.env` file.
The shared Compose base passes all market-data settings to `fleet-api` in
both local development and packaged deployments. Recreate the API container for
the environment change to apply. For installed deployments, use the normal
`./run-fleet.sh` workflow so host-profile env-file layering is preserved; a plain
container restart does not apply changed environment variables. Then reload the
dashboard.

For local development from the repository root, first load and export
`MARKET_DATA_COINGECKO_API_KEY` from your protected environment file, then run:

```sh
source bin/activate-hermit
MARKET_DATA_ENABLED=true just dev
```

Remove the override or set it to `false` and recreate the API container to disable
the feature and its outbound traffic again. An already-open enabled dashboard
hides the section when its next poll observes the disabled response.

## Verification

Unit tests use local HTTP fixtures and cover payload validation, partial outages,
timeouts, calculation units, cache expiry, cancellation, concurrent readers, and
permission gates. Client tests cover formatting, stale/empty states, polling,
hidden tabs, cancellation, retry, and default-off rollout behavior. Storybook
provides current, checking-enablement, stale, partial, unavailable, and disabled previews.

An optional live provider smoke test exercises CoinGecko Pro and Mempool without
starting the database or fleet server. Export your paid-plan API key through
`MARKET_DATA_COINGECKO_API_KEY` first:

```sh
source bin/activate-hermit
MARKET_DATA_LIVE_TEST=1 go test ./server/internal/domain/marketdata -run TestHTTPProviderLive -count=1
```

This makes three outbound requests and is intentionally skipped by normal tests.
