/**
 * Strategy 2 — Proxy to miner, versioned clients.
 *
 * Probe the miner's version, then mount the matching bespoke client
 * (ProtoOSv1Mini / ProtoOSv2Mini). Deliberately does NOT use the shared view —
 * the point is rendering per-version clients verbatim, which is exactly the
 * maintenance cost this strategy trades for firmware fidelity.
 *
 * In the Lab the mini clients hit the fake rigs directly; in production the same
 * clients would target the minerproxy path (/api-proxy/miners/:id), so no
 * browser CORS/TLS and no direct device exposure.
 */
import { useCallback, useState } from "react";

import { getJson, normalizeBaseUrl } from "../adapter/http";
import { ProtoOSv1Mini } from "./ProtoOSv1Mini";
import { ProtoOSv2Mini } from "./ProtoOSv2Mini";

const PRESETS = [
  { label: "Fake rig · MDK v1", url: "http://localhost:18081" },
  { label: "Fake rig · MDK v2", url: "http://localhost:18082" },
];
const DEFAULT_PASSWORD = "admin1234";

interface Mount {
  version: "1" | "2";
  baseUrl: string;
}

export default function ProxyVersionedPage() {
  const [rawUrl, setRawUrl] = useState(PRESETS[0].url);
  const [password, setPassword] = useState(DEFAULT_PASSWORD);
  const [mount, setMount] = useState<Mount | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const connect = useCallback(async () => {
    setBusy(true);
    setError(null);
    setMount(null);
    try {
      const baseUrl = normalizeBaseUrl(rawUrl);
      let version: "1" | "2" = "1";
      try {
        const v = await getJson<{ apiVersions: string[]; mdkVersion: string }>(`${baseUrl}/api/version`);
        version = v.apiVersions.includes("v2") || v.mdkVersion === "2" ? "2" : "1";
      } catch {
        version = "1"; // pre-probe firmware → legacy v1 client
      }
      setMount({ version, baseUrl });
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, [rawUrl]);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-3 rounded-lg border border-border-5 bg-surface-elevated-base p-4">
        <div className="flex flex-wrap items-end gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-heading-100 tracking-wide text-text-primary-50 uppercase">
              Miner base URL / minerproxy path
            </span>
            <input
              value={rawUrl}
              onChange={(e) => setRawUrl(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && connect()}
              className="w-80 rounded-md border border-border-5 bg-surface-base px-3 py-1.5 text-200 text-text-primary"
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-heading-100 tracking-wide text-text-primary-50 uppercase">Password (v1)</span>
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
            {busy ? "…" : "Detect & mount"}
          </button>
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
          {mount ? (
            <span className="ml-auto text-heading-100 text-text-primary-50">
              Mounted <span className="text-text-primary">ProtoOS v{mount.version}</span> client
            </span>
          ) : null}
        </div>
      </div>

      {error ? (
        <div className="rounded-lg border border-intent-critical-10 bg-intent-critical-10 p-3 text-200 text-text-critical">
          {error}
        </div>
      ) : null}

      {mount ? (
        mount.version === "2" ? (
          <ProtoOSv2Mini baseUrl={mount.baseUrl} />
        ) : (
          <ProtoOSv1Mini baseUrl={mount.baseUrl} password={password} />
        )
      ) : !error && !busy ? (
        <div className="rounded-lg border border-dashed border-border-5 p-6 text-200 text-text-primary-50">
          Detect a miner to mount its version-specific client. Compare v1 (:18081) vs v2 (:18082) — two different UIs.
        </div>
      ) : null}
    </div>
  );
}
