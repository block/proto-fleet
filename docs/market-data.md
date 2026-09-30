# Bitcoin market data

When enabled, the ProtoFleet dashboard displays bitcoin's USD spot price,
estimated USD hashprice per PH/s per day, and estimated Bitcoin network hashrate in EH/s.
Market data is global, independent of the selected site and telemetry duration.
It is available to authenticated users with `fleet:read` at any site or across
the organization.

The feature is **off by default**. `MARKET_DATA_ENABLED=true` enables both the
dashboard section and server-side feed requests without a client rebuild.

## Sources and units

The following public responses were verified on September 30, 2026:

| Input                | Source        | Endpoint                                               | Units / interpretation                                                                              |
| -------------------- | ------------- | ------------------------------------------------------ | --------------------------------------------------------------------------------------------------- |
| Spot price           | Coinbase      | `https://api.coinbase.com/v2/prices/BTC-USD/spot`      | `data.amount`: string, USD per BTC; verify `base=BTC` and `currency=USD`                            |
| Network / difficulty | mempool.space | `https://mempool.space/api/v1/mining/hashrate/3d`      | `currentHashrate`: H/s; `currentDifficulty`: dimensionless Bitcoin difficulty                       |
| Gross block rewards  | mempool.space | `https://mempool.space/api/v1/mining/reward-stats/144` | `totalReward`: satoshis including subsidy and transaction fees; inclusive `startBlock` / `endBlock` |

These endpoints worked without API keys. Requests originate from fleetd,
not each user's browser, and contain no fleet, site, miner, or user information.
Normal server-side egress metadata (including the server's IP address) is visible
to the providers. No provider uptime, price-publication cadence, or redistribution
rights are assumed. Confirm applicable provider terms and capacity before a
large production rollout.

Primary references:

- [Coinbase price API](https://docs.cdp.coinbase.com/coinbase-app/track-apis/prices)
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

| Environment variable           | Default                    | Purpose                                                                          |
| ------------------------------ | -------------------------- | -------------------------------------------------------------------------------- |
| `MARKET_DATA_ENABLED`          | `false`                    | Opt in with `true` to show the dashboard section and allow public feed requests |
| `MARKET_DATA_REFRESH_INTERVAL` | `1m`                       | Minimum upstream interval; allowed range `1m`–`15m`                              |
| `MARKET_DATA_COINBASE_URL`     | `https://api.coinbase.com` | Coinbase-compatible API origin / optional base path                              |
| `MARKET_DATA_MEMPOOL_URL`      | `https://mempool.space`    | Mempool-compatible API origin / optional base path                               |

CLI equivalents are `--market-data-enabled=true`,
`--market-data-refresh-interval=5m`, `--market-data-coinbase-url=...`, and
`--market-data-mempool-url=...`. URLs must use HTTP(S) and cannot contain embedded
credentials, queries, or fragments. Use HTTPS for public providers.

A self-hosted Mempool **mainnet** instance must have mining indexing enabled and
serve both mining endpoints. Alternative origins must preserve the API semantics;
do not point them at testnet, signet, or regtest. Source labels name the upstream
API implementation, not the configured hostname.

No database migration, API key, or new dependency is needed.

### Enable in a dev deployment

After deploying a build containing this feature, set this in the deployment's
operator `.env`:

```sh
MARKET_DATA_ENABLED=true
```

The shared Compose base passes all four market-data settings to `fleet-api` in
both local development and packaged deployments. Recreate the API container for
the environment change to apply. For installed deployments, use the normal
`./run-fleet.sh` workflow so host-profile env-file layering is preserved; a plain
container restart does not apply changed environment variables. Then reload the
dashboard.

For local development from the repository root:

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

An optional live provider smoke test exercises the real adapters without starting
the database or fleet server:

```sh
source bin/activate-hermit
MARKET_DATA_LIVE_TEST=1 go test ./server/internal/domain/marketdata -run TestHTTPProviderLive -count=1
```

This makes three outbound requests and is intentionally skipped by normal tests.
