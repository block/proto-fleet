import { useEffect, useState } from "react";
import { API_PROXY_BASE } from "@/protoFleet/api/constants";
import { ALERTS_ENABLED } from "@/protoFleet/constants/featureFlags";

const ENABLED_URL = `${API_PROXY_BASE}/api/v1/alerts/enabled`;

// Module-level cache so the probe runs once per session regardless of how many
// components mount the hook (mirrors the firmware-config fetch pattern).
let cache: boolean | null = null;
let inflight: Promise<boolean | null> | null = null;

// A blip while probing would otherwise hide the alerts surface for the session: this hook is mounted by the
// shell, which no route change remounts. So an unanswered probe is retried for as long as it stays mounted,
// backing off to a slow poll that costs one small GET.
const PROBE_RETRY_MS = 2_000;
const PROBE_RETRY_MAX_MS = 60_000;
// A connection held open without ever answering is the one failure the retry loop can't see: it waits on this
// promise, and every later probe joins the same one. A deadline turns that into an ordinary unanswered probe.
const PROBE_TIMEOUT_MS = 10_000;

// null when nothing was learned — no answer arrived, which is not the same as answering "disabled". The
// endpoint reports both states as a 200 body, so any other status is a failed probe rather than an answer.
async function fetchAlertsEnabled(): Promise<boolean | null> {
  if (cache !== null) return cache;
  if (inflight) return inflight;
  inflight = (async () => {
    const deadline = new AbortController();
    const deadlineId = setTimeout(() => deadline.abort(), PROBE_TIMEOUT_MS);
    try {
      const response = await fetch(ENABLED_URL, { credentials: "include", signal: deadline.signal });
      if (!response.ok) return null;
      cache = (await response.json())?.enabled === true;
      return cache;
    } catch {
      return null;
    } finally {
      clearTimeout(deadlineId);
      inflight = null;
    }
  })();
  return inflight;
}

// Exists so tests can force a re-probe.
export function _resetAlertsEnabledCache(): void {
  cache = null;
  inflight = null;
}

export interface AlertsEnabledState {
  enabled: boolean;
  // False only while no probe has answered yet: "not enabled" then means "unknown", not "off".
  resolved: boolean;
}

/**
 * Whether the Alerts feature is available, decided at runtime by the
 * server (the Grafana sidecar this feature proxies). The released client is a
 * prebuilt bundle, so this can't be a build-time flag — the server reports it.
 * `ALERTS_ENABLED` stays as a build-time override for QA/dogfood.
 */
export function useAlertsEnabledState(): AlertsEnabledState {
  // Same shape as the answered case below, so a build forcing alerts on keeps them on across a remount that
  // reads a cached "disabled" rather than re-probing.
  const [enabled, setEnabled] = useState<boolean>((cache ?? false) || ALERTS_ENABLED);
  const [resolved, setResolved] = useState<boolean>(cache !== null || ALERTS_ENABLED);
  // No short-circuit on an already-cached answer: effects run after the render that seeded state, so another
  // consumer's probe can answer in between, and returning on it would leave the initializer's "disabled" to
  // outlive the answer and hide the alerts surface for this mount's whole life. A cached answer costs no
  // request — fetchAlertsEnabled returns it directly — and re-setting an unchanged value is a no-op in React.
  useEffect(() => {
    let active = true;
    let retryId: ReturnType<typeof setTimeout> | null = null;

    const probe = async (retryMs: number) => {
      const answer = await fetchAlertsEnabled();
      if (!active) return;
      if (answer !== null) {
        setEnabled(answer || ALERTS_ENABLED);
        setResolved(true);
        return;
      }
      retryId = setTimeout(() => void probe(Math.min(retryMs * 2, PROBE_RETRY_MAX_MS)), retryMs);
    };

    void probe(PROBE_RETRY_MS);
    return () => {
      active = false;
      if (retryId !== null) clearTimeout(retryId);
    };
  }, []);
  return { enabled, resolved };
}

// The common consumer shape: chrome that hides the alerts surface treats "unknown" the same as "off".
export function useAlertsEnabled(): boolean {
  return useAlertsEnabledState().enabled;
}
