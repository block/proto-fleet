/**
 * ProtoOS v1 mini client (Strategy 2).
 *
 * A deliberately bespoke, version-specific client — NOT the shared view. It
 * renders the v1 REST surface in its own idiom (many endpoints, board hardware
 * list + aggregate mining stats, no per-chip grid because v1 doesn't expose
 * one). This is the strategy's whole point and cost: you ship and maintain a
 * separate client per firmware generation.
 *
 * Fetches go through `baseUrl`. In the Lab that's the fake rig directly; in
 * production it would be the minerproxy path (/api-proxy/miners/:id), same code.
 */
import { useCallback, useEffect, useRef, useState } from "react";

import { getJson, postJson } from "../adapter/http";

interface V1Data {
  model: string;
  serial: string;
  firmware: string;
  hostname: string;
  status: string;
  hashrateThs: number;
  powerW: number;
  hbTempC: number;
  asicTempC: number;
  boards: Array<{ slot: number; serial: string; asics: number }>;
}

async function fetchV1(baseUrl: string, password: string, signal: AbortSignal): Promise<V1Data> {
  const login = await postJson<{ access_token: string }>(`${baseUrl}/api/v1/auth/login`, { password }, { signal });
  const token = login.access_token;
  const [system, mining, boards] = await Promise.all([
    getJson<{ "system-info": { model?: string; cb_sn: string; os: { version: string; hostname: string } } }>(
      `${baseUrl}/api/v1/system`,
      { signal, token },
    ),
    getJson<{
      "mining-status": {
        status: string;
        hashrate_ghs: number;
        power_usage_watts: number;
        average_hb_temp_c: number;
        average_asic_temp_c: number;
      };
    }>(`${baseUrl}/api/v1/mining`, { signal, token }),
    getJson<{ "hashboards-info": Array<{ slot: number; hb_sn?: string; mining_asic_count?: number }> }>(
      `${baseUrl}/api/v1/hashboards`,
      { signal, token },
    ),
  ]);
  const s = system["system-info"];
  const m = mining["mining-status"];
  return {
    model: s.model ?? "Proto",
    serial: s.cb_sn,
    firmware: s.os.version,
    hostname: s.os.hostname,
    status: m.status,
    hashrateThs: m.hashrate_ghs / 1000,
    powerW: m.power_usage_watts,
    hbTempC: m.average_hb_temp_c,
    asicTempC: m.average_asic_temp_c,
    boards: (boards["hashboards-info"] ?? []).map((b) => ({
      slot: b.slot,
      serial: b.hb_sn ?? `HB-${b.slot}`,
      asics: b.mining_asic_count ?? 0,
    })),
  };
}

export function ProtoOSv1Mini({ baseUrl, password }: { baseUrl: string; password: string }) {
  const [data, setData] = useState<V1Data | null>(null);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  const load = useCallback(() => {
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    fetchV1(baseUrl, password, ctrl.signal)
      .then((d) => {
        setData(d);
        setError(null);
      })
      .catch((e) => {
        if (!ctrl.signal.aborted) setError(e instanceof Error ? e.message : String(e));
      });
  }, [baseUrl, password]);

  useEffect(() => {
    load();
    return () => abortRef.current?.abort();
  }, [load]);

  if (error) return <div className="text-200 text-text-critical">{error}</div>;
  if (!data) return <div className="text-200 text-text-primary-50">Loading ProtoOS v1…</div>;

  return (
    <div className="flex flex-col gap-4 rounded-lg border-2 border-intent-info-10 bg-surface-base p-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className="rounded bg-intent-info-fill/80 px-2 py-0.5 text-heading-100 text-text-primary">
            ProtoOS v1
          </span>
          <span className="text-heading-200 text-text-primary">{data.hostname}</span>
        </div>
        <span className="text-200 text-text-primary-50">{data.status}</span>
      </div>

      <dl className="grid grid-cols-2 gap-x-6 gap-y-1 text-200 sm:grid-cols-4">
        <div>
          <dt className="text-heading-100 text-text-primary-50">Hashrate</dt>
          <dd className="text-text-primary">{data.hashrateThs.toFixed(1)} TH/s</dd>
        </div>
        <div>
          <dt className="text-heading-100 text-text-primary-50">Power</dt>
          <dd className="text-text-primary">{Math.round(data.powerW)} W</dd>
        </div>
        <div>
          <dt className="text-heading-100 text-text-primary-50">HB temp</dt>
          <dd className="text-text-primary">{data.hbTempC.toFixed(1)} °C</dd>
        </div>
        <div>
          <dt className="text-heading-100 text-text-primary-50">ASIC temp (avg)</dt>
          <dd className="text-text-primary">{data.asicTempC.toFixed(1)} °C</dd>
        </div>
      </dl>

      <div>
        <div className="mb-1 text-heading-100 tracking-wide text-text-primary-50 uppercase">
          Hashboards (hardware list — v1 exposes no per-chip telemetry)
        </div>
        <table className="w-full text-left text-200">
          <thead className="text-heading-100 text-text-primary-50">
            <tr>
              <th className="py-1">Slot</th>
              <th>Serial</th>
              <th>ASIC count</th>
            </tr>
          </thead>
          <tbody className="text-text-primary">
            {data.boards.map((b) => (
              <tr key={b.slot} className="border-t border-border-5">
                <td className="py-1">{b.slot}</td>
                <td>{b.serial}</td>
                <td>{b.asics}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="text-heading-100 text-text-primary-30">
        {data.model} · fw {data.firmware} · SN {data.serial}
      </div>
    </div>
  );
}
