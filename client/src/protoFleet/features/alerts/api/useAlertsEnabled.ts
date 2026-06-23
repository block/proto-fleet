import { useEffect, useState } from "react";
import { API_PROXY_BASE } from "@/protoFleet/api/constants";
import { NOTIFICATIONS_ENABLED } from "@/protoFleet/constants/featureFlags";

const ENABLED_URL = `${API_PROXY_BASE}/api/v1/alerts/enabled`;

// Module-level cache so the probe runs once per session regardless of how many
// components mount the hook (mirrors the firmware-config fetch pattern).
let cache: boolean | null = null;
let inflight: Promise<boolean> | null = null;

async function fetchAlertsEnabled(): Promise<boolean> {
  if (cache !== null) return cache;
  if (inflight) return inflight;
  inflight = (async () => {
    try {
      const response = await fetch(ENABLED_URL, { credentials: "include" });
      cache = response.ok && (await response.json())?.enabled === true;
      return cache;
    } catch {
      return false;
    } finally {
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

/**
 * Whether the Alerts feature is available, decided at runtime by the
 * server (the Grafana sidecar this feature proxies). The released client is a
 * prebuilt bundle, so this can't be a build-time flag — the server reports it.
 * `NOTIFICATIONS_ENABLED` stays as a build-time override for QA/dogfood.
 */
export function useAlertsEnabled(): boolean {
  const [enabled, setEnabled] = useState<boolean>(cache ?? NOTIFICATIONS_ENABLED);
  useEffect(() => {
    let active = true;
    void fetchAlertsEnabled().then((value) => {
      if (active) setEnabled(value || NOTIFICATIONS_ENABLED);
    });
    return () => {
      active = false;
    };
  }, []);
  return enabled;
}
