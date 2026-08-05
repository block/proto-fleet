/**
 * Strategy 1 — Fleet-native + single-miner mode.
 *
 * The entry IS the single-miner mode: type a miner IP, and we resolve it purely
 * through the fleet server (ListMinerStateSnapshots by /32) and render the
 * shared <SingleMinerView>. No call ever touches the device directly.
 *
 * Identity + KPIs are real fleet data. The ASIC grid is synthesized — see
 * fleetAdapter.ts for exactly why (fleet collects components but discards them
 * at persistence; a `prototype/v1` RPC would make the grid real).
 */
import { useCallback, useEffect, useRef, useState } from "react";

import { SingleMinerView } from "../shared/SingleMinerView";
import type { SingleMinerSnapshot } from "../shared/types";
import { FleetAdapter } from "./fleetAdapter";
import { fleetManagementClient } from "@/protoFleet/api/clients";

export default function FleetNativePage() {
  const [ip, setIp] = useState("");
  const [connected, setConnected] = useState<string | null>(null);
  const [snapshot, setSnapshot] = useState<SingleMinerSnapshot | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const abortRef = useRef<AbortController | null>(null);

  const load = useCallback(async (target: string) => {
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    setBusy(true);
    setError(null);
    try {
      const snap = await new FleetAdapter(target).fetchSnapshot(ctrl.signal);
      setSnapshot(snap);
      setConnected(target);
    } catch (e) {
      if (!ctrl.signal.aborted) {
        setError(e instanceof Error ? e.message : String(e));
        setSnapshot(null);
      }
    } finally {
      if (abortRef.current === ctrl) setBusy(false);
    }
  }, []);

  // Convenience for the demo: grab any discovered miner's IP so there's always a
  // valid target without knowing the fake fleet's addressing.
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
      setIp(found);
      await load(found);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setBusy(false);
    }
  }, [load]);

  useEffect(() => () => abortRef.current?.abort(), []);

  return (
    <div className="flex flex-col gap-4">
      {/* Single-miner mode entry */}
      <div className="flex flex-col gap-3 rounded-lg border border-border-5 bg-surface-elevated-base p-4">
        <span className="text-heading-100 tracking-wide text-text-primary-50 uppercase">
          Single-miner mode — enter a miner IP
        </span>
        <div className="flex flex-wrap items-end gap-3">
          <input
            value={ip}
            onChange={(e) => setIp(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && ip && load(ip)}
            placeholder="192.168.2.30"
            className="w-72 rounded-md border border-border-5 bg-surface-base px-3 py-1.5 text-200 text-text-primary"
          />
          <button
            type="button"
            onClick={() => ip && load(ip)}
            disabled={busy || !ip}
            className="bg-emphasis-300 rounded-md px-4 py-1.5 text-200 text-text-primary hover:opacity-90 disabled:opacity-50"
          >
            {busy ? "…" : "Open miner"}
          </button>
          <button
            type="button"
            onClick={pickDiscovered}
            disabled={busy}
            className="rounded-md border border-border-5 bg-surface-5 px-3 py-1.5 text-200 text-text-primary hover:bg-surface-base disabled:opacity-50"
          >
            Pick a discovered miner
          </button>
          {connected ? (
            <button
              type="button"
              onClick={() => load(connected)}
              disabled={busy}
              className="rounded-md border border-border-5 bg-surface-5 px-3 py-1.5 text-200 text-text-primary hover:bg-surface-base disabled:opacity-50"
            >
              Refresh
            </button>
          ) : null}
        </div>
        <span className="text-heading-100 text-text-primary-50">
          Resolves via <code>ListMinerStateSnapshots</code> over <code>/api-proxy</code> — fleet-native, never touches
          the device. ASIC grid is synthesized (fleet discards component arrays at persistence).
        </span>
      </div>

      {error ? (
        <div className="rounded-lg border border-intent-critical-10 bg-intent-critical-10 p-3 text-200 text-text-critical">
          {error}
        </div>
      ) : null}

      {snapshot ? (
        <SingleMinerView snapshot={snapshot} />
      ) : !error && !busy ? (
        <div className="rounded-lg border border-dashed border-border-5 p-6 text-200 text-text-primary-50">
          Enter a miner IP (or pick a discovered one) to render it fleet-native.
        </div>
      ) : null}
    </div>
  );
}
