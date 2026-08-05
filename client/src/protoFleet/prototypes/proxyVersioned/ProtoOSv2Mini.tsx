/**
 * ProtoOS v2 mini client (Strategy 2).
 *
 * A second, separately-maintained version-specific client. It speaks the v2
 * consolidated envelope (one call) and renders a native per-chip grid — a shape
 * v1 can't produce. Visually and structurally distinct from ProtoOSv1Mini on
 * purpose: this is the maintenance cost the strategy makes concrete.
 */
import { useCallback, useEffect, useRef, useState } from "react";

import { getJson } from "../adapter/http";

interface V2Chip {
  pos: number;
  tempC: number;
  state: string;
}
interface V2Envelope {
  data: {
    device: { displayName: string; hardwareModel: string; firmwareRev: string; unitSerial: string };
    state: string;
    performance: { hashrateGhs: number; powerWatts: number; thermals: { peakC: number } };
    boards: Array<{ slot: number; serial: string; hashrateGhs: number; chips: V2Chip[] }>;
  };
}

const CHIP_COLOR: Record<string, string> = {
  ONLINE: "bg-intent-success-fill/80",
  HOT: "bg-intent-warning-fill/80",
  FAULT: "bg-intent-critical-fill/80",
  OFFLINE: "bg-surface-5",
};

export function ProtoOSv2Mini({ baseUrl }: { baseUrl: string }) {
  const [env, setEnv] = useState<V2Envelope | null>(null);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  const load = useCallback(() => {
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    getJson<V2Envelope>(`${baseUrl}/api/v2/miner`, { signal: ctrl.signal })
      .then((e) => {
        setEnv(e);
        setError(null);
      })
      .catch((e) => {
        if (!ctrl.signal.aborted) setError(e instanceof Error ? e.message : String(e));
      });
  }, [baseUrl]);

  useEffect(() => {
    load();
    return () => abortRef.current?.abort();
  }, [load]);

  if (error) return <div className="text-200 text-text-critical">{error}</div>;
  if (!env) return <div className="text-200 text-text-primary-50">Loading ProtoOS v2…</div>;

  const d = env.data;
  return (
    <div className="border-emphasis-300 flex flex-col gap-4 rounded-lg border-2 bg-surface-base p-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className="bg-emphasis-300 rounded px-2 py-0.5 text-heading-100 text-text-primary">ProtoOS v2</span>
          <span className="text-heading-200 text-text-primary">{d.device.displayName}</span>
        </div>
        <span className="text-200 text-text-primary-50">{d.state}</span>
      </div>

      <div className="flex flex-wrap gap-6 text-200">
        <span className="text-text-primary">
          {(d.performance.hashrateGhs / 1000).toFixed(1)} <span className="text-text-primary-50">TH/s</span>
        </span>
        <span className="text-text-primary">
          {Math.round(d.performance.powerWatts)} <span className="text-text-primary-50">W</span>
        </span>
        <span className="text-text-primary">
          {d.performance.thermals.peakC.toFixed(1)} <span className="text-text-primary-50">°C peak</span>
        </span>
      </div>

      <div className="flex flex-col gap-3">
        <div className="text-heading-100 tracking-wide text-text-primary-50 uppercase">
          Per-chip grid (native to v2 — one consolidated call)
        </div>
        {d.boards.map((b) => (
          <div key={b.slot} className="flex flex-col gap-1">
            <div className="flex justify-between text-heading-100 text-text-primary-50">
              <span>
                Board {b.slot} · {b.serial}
              </span>
              <span>{(b.hashrateGhs / 1000).toFixed(1)} TH/s</span>
            </div>
            <div className="flex flex-wrap gap-1">
              {b.chips.map((c) => (
                <div
                  key={c.pos}
                  title={`chip ${c.pos} · ${c.tempC.toFixed(0)} °C · ${c.state}`}
                  className={`h-4 w-4 rounded-sm ${CHIP_COLOR[c.state] ?? "bg-surface-5"}`}
                />
              ))}
            </div>
          </div>
        ))}
      </div>

      <div className="text-heading-100 text-text-primary-30">
        {d.device.hardwareModel} · fw {d.device.firmwareRev} · SN {d.device.unitSerial}
      </div>
    </div>
  );
}
