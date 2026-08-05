/**
 * Strategy 2 — Proxy to miner, versioned clients.
 *
 * Placeholder. Will detect the miner's MDK version and mount the matching mini
 * client (ProtoOSv1Mini / ProtoOSv2Mini), each pointed at the reverse proxy
 * (/api-proxy/miners/:id). Deliberately does NOT use the shared view — the
 * point of this strategy is rendering per-version clients verbatim.
 */
export default function ProxyVersionedPage() {
  return (
    <div className="rounded-lg border border-dashed border-border-5 p-6 text-200 text-text-primary-50">
      Strategy 2 (proxy, versioned) — coming next. Will mount ProtoOSv1Mini / ProtoOSv2Mini based on the miner's
      detected MDK version, each proxied to the device via the existing minerproxy.
    </div>
  );
}
