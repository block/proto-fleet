/**
 * Tiny fetch helpers for the direct browser→miner adapters. Deliberately
 * dependency-free — the whole point of Strategy 3's MDK adapters is that they
 * talk to the device with nothing between the browser and the miner.
 */

export class MinerHttpError extends Error {
  constructor(
    readonly status: number,
    readonly url: string,
    body: string,
  ) {
    super(`${status} ${url}${body ? ` — ${body.slice(0, 200)}` : ""}`);
    this.name = "MinerHttpError";
  }
}

interface RequestOpts {
  signal?: AbortSignal;
  token?: string;
}

async function request<T>(method: string, url: string, body: unknown, opts: RequestOpts): Promise<T> {
  const headers: Record<string, string> = {};
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (opts.token) headers.Authorization = `Bearer ${opts.token}`;

  const res = await fetch(url, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: opts.signal,
  });

  if (!res.ok) {
    throw new MinerHttpError(res.status, url, await res.text().catch(() => ""));
  }
  // 202/204 responses (reboot, logout) may carry no JSON body.
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

export function getJson<T>(url: string, opts: RequestOpts = {}): Promise<T> {
  return request<T>("GET", url, undefined, opts);
}

export function postJson<T>(url: string, body: unknown, opts: RequestOpts = {}): Promise<T> {
  return request<T>("POST", url, body, opts);
}

/** Strip a trailing slash so `${base}/api/...` never double-slashes. */
export function normalizeBaseUrl(raw: string): string {
  const trimmed = raw.trim().replace(/\/+$/, "");
  // Bare host/IP → default to http (fake rigs and lab miners are plain HTTP).
  if (!/^https?:\/\//i.test(trimmed)) return `http://${trimmed}`;
  return trimmed;
}

/** Best-effort host label for the identity card. */
export function hostOf(baseUrl: string): string {
  try {
    return new URL(baseUrl).host;
  } catch {
    return baseUrl;
  }
}
