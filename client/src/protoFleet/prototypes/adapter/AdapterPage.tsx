/**
 * Strategy 3 — Abstraction layer / adapters.
 *
 * One generic <SingleMinerView>, swappable backend adapters. Pick a backend:
 *
 *   - Fleet server → FleetAdapter (identity + KPIs from ListMinerStateSnapshots
 *     over /api-proxy; ASIC grid synthesized — same gap as Strategy 1). This is
 *     what a single-miner UI rendered *inside ProtoFleet off the fleet backend*
 *     looks like.
 *   - MDK miner → probe /api/version and resolve MdkV1Adapter / MdkV2Adapter,
 *     talking straight to the device.
 *
 * All three fold their very different backends into the identical snapshot and
 * render the same view — that's the whole thesis.
 */
import { useCallback, useEffect, useRef, useState } from "react";

import { FleetAdapter } from "../fleetNative/fleetAdapter";
import type { SingleMinerAdapter } from "../shared/adapter";
import { SingleMinerView } from "../shared/SingleMinerView";
import type { SingleMinerSnapshot } from "../shared/types";
import { normalizeBaseUrl } from "./http";
import { probeAndResolve } from "./probe";
import { fleetManagementClient } from "@/protoFleet/api/clients";

type Backend = "fleet" | "mdk";

const MDK_PRESETS = [
  { label: "Fake rig · MDK v1", url: "http://localhost:18081" },
  { label: "Fake rig · MDK v2", url: "http://localhost:18082" },
];
const DEFAULT_PASSWORD = "admin1234";

interface Connection {
  adapter: SingleMinerAdapter;
  detail: string;
}

export default function AdapterPage() {
  const [backend, setBackend] = useState<Backend>("fleet");
  const [fleetIp, setFleetIp] = useState("");
  const [rawUrl, setRawUrl] = useState(MDK_PRESETS[0].url);
  const [password, setPassword] = useState(DEFAULT_PASSWORD);
  const [connection, setConnection] = useState<Connection | null>(null);
  const [snapshot, setSnapshot] = useState<SingleMinerSnapshot | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const abortRef = useRef<AbortController | null>(null);

  const load = useCallback(async (adapter: SingleMinerAdapter) => {
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    setBusy(true);
    setError(null);
    try {
      setSnapshot(await adapter.fetchSnapshot(ctrl.signal));
    } catch (e) {
      if (!ctrl.signal.aborted) setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (abortRef.current === ctrl) setBusy(false);
    }
  }, []);

  const connectFleet = useCallback(
    async (target: string) => {
      setBusy(true);
      setError(null);
      setSnapshot(null);
      const adapter = new FleetAdapter(target);
      setConnection({ adapter, detail: "fleet-native · grid synthesized" });
      await load(adapter);
    },
    [load],
  );

  const connectMdk = useCallback(async () => {
    setBusy(true);
    setError(null);
    setSnapshot(null);
    setConnection(null);
    try {
      const baseUrl = normalizeBaseUrl(rawUrl);
      const resolved = await probeAndResolve(baseUrl, password);
      setConnection({ adapter: resolved.adapter, detail: `MDK v${resolved.mdkVersion} · fw ${resolved.firmwareRev}` });
      await load(resolved.adapter);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setBusy(false);
    }
  }, [rawUrl, password, load]);

  // Fleet convenience: grab any discovered miner so there's always a valid IP.
  const pickDiscovered = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await fleetManagementClient.listMinerStateSnapshots({ pageSize: 1 });
      const found = res.miners[0]?.ipAddress;
      if (!found) {
        setError("No discovered miners in the fleet.");
        setBusy(false);
        return;
      }
      setFleetIp(found);
      await connectFleet(found);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setBusy(false);
    }
  }, [connectFleet]);

  const switchBackend = useCallback((next: Backend) => {
    setBackend(next);
    setConnection(null);
    setSnapshot(null);
    setError(null);
  }, []);

  useEffect(() => () => abortRef.current?.abort(), []);

  return (
    <div className="flex flex-col gap-4">
      {/* Backend selector */}
      <div className="flex flex-col gap-3 rounded-lg border border-border-5 bg-surface-elevated-base p-4">
        <div className="flex gap-2">
          {(["fleet", "mdk"] as Backend[]).map((b) => (
            <button
              key={b}
              type="button"
              onClick={() => switchBackend(b)}
              className={`rounded-md px-3 py-1.5 text-200 ${
                backend === b
                  ? "bg-emphasis-300 text-text-primary"
                  : "border border-border-5 bg-surface-5 text-text-primary-50 hover:text-text-primary"
              }`}
            >
              {b === "fleet" ? "Fleet server" : "MDK miner (direct)"}
            </button>
          ))}
        </div>

        {backend === "fleet" ? (
          <div className="flex flex-col gap-2">
            <div className="flex flex-wrap items-end gap-3">
              <label className="flex flex-col gap-1">
                <span className="text-heading-100 tracking-wide text-text-primary-50 uppercase">Miner IP (fleet)</span>
                <input
                  value={fleetIp}
                  onChange={(e) => setFleetIp(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && fleetIp && connectFleet(fleetIp)}
                  placeholder="192.168.2.30"
                  className="w-72 rounded-md border border-border-5 bg-surface-base px-3 py-1.5 text-200 text-text-primary"
                />
              </label>
              <button
                type="button"
                onClick={() => fleetIp && connectFleet(fleetIp)}
                disabled={busy || !fleetIp}
                className="bg-emphasis-300 rounded-md px-4 py-1.5 text-200 text-text-primary hover:opacity-90 disabled:opacity-50"
              >
                {busy ? "…" : "Render via fleet"}
              </button>
              <button
                type="button"
                onClick={pickDiscovered}
                disabled={busy}
                className="rounded-md border border-border-5 bg-surface-5 px-3 py-1.5 text-200 text-text-primary hover:bg-surface-base disabled:opacity-50"
              >
                Pick a discovered miner
              </button>
              {connection ? (
                <button
                  type="button"
                  onClick={() => load(connection.adapter)}
                  disabled={busy}
                  className="rounded-md border border-border-5 bg-surface-5 px-3 py-1.5 text-200 text-text-primary hover:bg-surface-base disabled:opacity-50"
                >
                  Refresh
                </button>
              ) : null}
            </div>
            <span className="text-heading-100 text-text-primary-50">
              Resolves via <code>ListMinerStateSnapshots</code> over <code>/api-proxy</code> — the fleet backend feeding
              the same generic view. ASIC grid synthesized (fleet discards component arrays at persistence).
            </span>
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            <div className="flex flex-wrap items-end gap-3">
              <label className="flex flex-col gap-1">
                <span className="text-heading-100 tracking-wide text-text-primary-50 uppercase">
                  Miner base URL / IP
                </span>
                <input
                  value={rawUrl}
                  onChange={(e) => setRawUrl(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && connectMdk()}
                  placeholder="192.168.1.42"
                  className="w-72 rounded-md border border-border-5 bg-surface-base px-3 py-1.5 text-200 text-text-primary"
                />
              </label>
              <label className="flex flex-col gap-1">
                <span className="text-heading-100 tracking-wide text-text-primary-50 uppercase">Password (v1)</span>
                <input
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && connectMdk()}
                  className="w-40 rounded-md border border-border-5 bg-surface-base px-3 py-1.5 text-200 text-text-primary"
                />
              </label>
              <button
                type="button"
                onClick={connectMdk}
                disabled={busy}
                className="bg-emphasis-300 rounded-md px-4 py-1.5 text-200 text-text-primary hover:opacity-90 disabled:opacity-50"
              >
                {busy ? "…" : "Connect"}
              </button>
              {connection ? (
                <button
                  type="button"
                  onClick={() => load(connection.adapter)}
                  disabled={busy}
                  className="rounded-md border border-border-5 bg-surface-5 px-3 py-1.5 text-200 text-text-primary hover:bg-surface-base disabled:opacity-50"
                >
                  Refresh
                </button>
              ) : null}
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-heading-100 text-text-primary-50">Presets:</span>
              {MDK_PRESETS.map((p) => (
                <button
                  key={p.url}
                  type="button"
                  onClick={() => setRawUrl(p.url)}
                  className="rounded-full border border-border-5 px-2.5 py-0.5 text-heading-100 text-text-primary-50 hover:text-text-primary"
                >
                  {p.label}
                </button>
              ))}
            </div>
          </div>
        )}

        {connection ? (
          <span className="text-heading-100 text-text-primary-50">
            Adapter: <span className="text-text-primary">{connection.adapter.source}</span> · {connection.detail}
          </span>
        ) : null}
      </div>

      {error ? (
        <div className="rounded-lg border border-intent-critical-10 bg-intent-critical-10 p-3 text-200 text-text-critical">
          {error}
          <div className="mt-1 text-heading-100 text-text-primary-50">
            MDK backend needs the fake rigs (<code>just lab-fakes</code>, v1 :18081 / v2 :18082). Fleet backend needs
            fleet-api up and an authenticated session.
          </div>
        </div>
      ) : null}

      {snapshot ? (
        <SingleMinerView
          snapshot={snapshot}
          actions={
            connection?.adapter.control
              ? { onControl: (a) => connection.adapter.control!(a).then(() => load(connection.adapter)) }
              : {}
          }
        />
      ) : !error && !busy ? (
        <div className="rounded-lg border border-dashed border-border-5 p-6 text-200 text-text-primary-50">
          Pick a backend and connect to render it through the resolved adapter — same view, three backends.
        </div>
      ) : null}
    </div>
  );
}
