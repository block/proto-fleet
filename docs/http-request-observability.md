# HTTP request observability

Fleet's existing OpenTelemetry HTTP server span records response status and
duration. Unary Connect handlers classify that span before admission,
authentication and application execution. Rollout REST adapters use the same
classification before decoding:

| `resource.name` | Population |
| --- | --- |
| `fleet.api.unary` | Unary RPCs outside FleetNodeGatewayService and registered rollout REST adapters, including decoding failures |
| `fleet.node.unary` | Unary FleetNodeGatewayService calls, including node heartbeats |

The explicit Datadog resource override preserves normal HTTP span naming, and
`http.route` records the registered RPC procedure. Classification adds no spans
and uses two fixed resource names;
no request IDs, organization IDs, miner IDs or raw URLs enter those names.
Rollout REST requests use their mapped RPC procedure for `http.route`, including
invalid bodies, queries and path IDs rejected before RPC dispatch. A forwarded
REST request reaches the unary interceptor with the same HTTP span; setting the
classification again updates its attributes without adding another span or
counting another request. Unary admission/authentication failures remain included.

Streams, health probes and other REST endpoints retain their existing HTTP spans
and are outside both unary classes. Direct RPC requests rejected before the
Connect interceptor runs are also outside this population.

The deployed Datadog collector's existing connector computes APM statistics from
received spans before indexed-trace retention. This allows the native
`trace.http.server.request.hits` count and `trace.http.server.request` duration
distribution to select `service:fleetd,env:<environment>,resource_name:fleet.api.unary`
without deriving error rates from retained/indexed span counts. Duration is in
seconds. HTTP 5xx failures can be counted from hits with `http.status_code:5*`;
4xx responses remain in total traffic but outside that server-failure numerator.

## Qualification before alert enablement

Release the instrumentation and verify the actual Datadog operation/resource
mapping before enabling downstream monitors. Confirm both new resource classes,
that health checks/streams/other REST traffic do not enter the API class, and that
rollout REST requests and real unary 5xx responses do enter it. Include rollout
REST decoding failures and admission/authentication failures. Confirm a forwarded
REST request produces one completed HTTP server span after both classification
calls, and that existing tracing/RUM correlation remains intact.
Do not change the Collector's metric mapping or invent a sampled-span fallback if
the expected native dimensions are absent.

Verify telemetry is enabled on every serving host, the SDK sampling configuration
is appropriate, and stats arrive from all expected versions during a rolling
release. Native stats cover received completed spans, not requests dropped before
tracing, an unavailable collector, an unsampled request, or a request still running.
Trusted incoming not-sampled trace flags can suppress spans. A traffic gate or quiet
error count is not an availability check; retain independent HA/synthetic coverage.

These aggregate HTTP measurements are not per-procedure SLOs or business/command
success metrics. They do not detect errors encoded in a successful HTTP response,
stream failures or stale telemetry. Different unary operation mixes can obscure a
slow low-volume RPC; separately qualify operation-specific or long-running work.
