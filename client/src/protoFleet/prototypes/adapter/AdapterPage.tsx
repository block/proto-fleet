/**
 * Strategy 3 — Abstraction layer / adapters.
 *
 * Point a base URL (or bare IP) at a miner, probe `/api/version`, resolve the
 * matching adapter (MDK v1 REST or MDK v2 consolidated), and render the exact
 * same <SingleMinerView>. Two very different wire formats, one view — that's the
 * whole thesis. Defaults target the two fake rigs from `just lab-fakes`, but any
 * reachable miner IP works (subject to the device sending CORS — see http.ts).
 */
import { useCallback, useEffect, useRef, useState } from "react";

import type { SingleMinerAdapter } from "../shared/adapter";
import { SingleMinerView } from "../shared/SingleMinerView";
import type { SingleMinerSnapshot } from "../shared/types";
import { normalizeBaseUrl } from "./http";
import { probeAndResolve } from "./probe";

const PRESETS = [
  { label: "Fake rig · MDK v1", url: "http://localhost:18081" },
  { label: "Fake rig · MDK v2", url: "http://localhost:18082" },
];
const DEFAULT_PASSWORD = "admin1234";

interface Connection {
  adapter: SingleMinerAdapter;
  mdkVersion: string;
  firmwareRev: string;
}

export default function AdapterPage() {
  const [rawUrl, setRawUrl] = useState(PRESETS[0].url);
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

  const connect = useCallback(async () => {
    setBusy(true);
    setError(null);
    setSnapshot(null);
    setConnection(null);
    try {
      const baseUrl = normalizeBaseUrl(rawUrl);
      const resolved = await probeAndResolve(baseUrl, password);
      setConnection(resolved);
      await load(resolved.adapter);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setBusy(false);
    }
  }, [rawUrl, password, load]);

  useEffect(() => () => abortRef.current?.abort(), []);

  return (
    <div className="flex flex-col gap-4">
      {/* Connection bar */}
      <div className="flex flex-col gap-3 rounded-lg border border-border-5 bg-surface-elevated-base p-4">
        <div className="flex flex-wrap items-end gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-heading-100 tracking-wide text-text-primary-50 uppercase">Miner base URL / IP</span>
            <input
              value={rawUrl}
              onChange={(e) => setRawUrl(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && connect()}
              placeholder="192.168.1.42"
              className="w-72 rounded-md border border-border-5 bg-surface-base px-3 py-1.5 text-200 text-text-primary"
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-heading-100 tracking-wide text-text-primary-50 uppercase">Password (v1 only)</span>
            <input
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && connect()}
              className="w-40 rounded-md border border-border-5 bg-surface-base px-3 py-1.5 text-200 text-text-primary"
            />
          </label>
          <button
            type="button"
            onClick={connect}
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
          {PRESETS.map((p) => (
            <button
              key={p.url}
              type="button"
              onClick={() => setRawUrl(p.url)}
              className="rounded-full border border-border-5 px-2.5 py-0.5 text-heading-100 text-text-primary-50 hover:text-text-primary"
            >
              {p.label}
            </button>
          ))}
          {connection ? (
            <span className="ml-auto text-heading-100 text-text-primary-50">
              Probed → <span className="text-text-primary">MDK v{connection.mdkVersion}</span> · fw{" "}
              {connection.firmwareRev} · adapter: <span className="text-text-primary">{connection.adapter.source}</span>
            </span>
          ) : null}
        </div>
      </div>

      {error ? (
        <div className="rounded-lg border border-intent-critical-10 bg-intent-critical-10 p-3 text-200 text-text-critical">
          {error}
          <div className="mt-1 text-heading-100 text-text-primary-50">
            Start the fake rigs with <code>just lab-fakes</code> (v1 :18081, v2 :18082). Direct browser→miner calls need
            CORS on the device.
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
      ) : null}

      {!snapshot && !error && !busy ? (
        <div className="rounded-lg border border-dashed border-border-5 p-6 text-200 text-text-primary-50">
          Connect to a fake rig (or real miner IP) to render it through the resolved adapter.
        </div>
      ) : null}
    </div>
  );
}
