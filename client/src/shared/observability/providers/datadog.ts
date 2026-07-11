import { datadogRum } from "@datadog/browser-rum";
import type { RumInitConfiguration } from "@datadog/browser-rum";

import { getConfigValue } from "../runtimeConfig";
import type { ObservabilityErrorMeta, ObservabilityInitContext, ObservabilityProvider } from "../types";

/** Required config keys — the provider is a no-op unless both are present. */
const REQUIRED_KEYS = ["DD_APPLICATION_ID", "DD_CLIENT_TOKEN"] as const;

const parseSampleRate = (value: string | undefined, fallback: number): number => {
  if (value === undefined) {
    return fallback;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
};

/** Distributed tracing scope: same-origin requests under the API base path
 * (e.g. `/api-proxy/...`). Datadog RUM injects trace headers on matching fetch
 * calls, so ConnectRPC RPCs — which go through the global fetch — are traced.
 * Matches the prefix on a path boundary so a sibling like `/api-proxy-internal`
 * is not swept in. */
const makeApiTracingMatcher =
  (apiTracingPathPrefix: string) =>
  (url: string): boolean => {
    try {
      const parsed = new URL(url, window.location.origin);
      if (parsed.origin !== window.location.origin) {
        return false;
      }
      return parsed.pathname === apiTracingPathPrefix || parsed.pathname.startsWith(`${apiTracingPathPrefix}/`);
    } catch {
      return false;
    }
  };

export const datadogProvider: ObservabilityProvider = {
  name: "datadog",

  isConfigured(): boolean {
    return REQUIRED_KEYS.every((key) => getConfigValue(key) !== undefined);
  },

  init(context: ObservabilityInitContext): void {
    const applicationId = getConfigValue("DD_APPLICATION_ID");
    const clientToken = getConfigValue("DD_CLIENT_TOKEN");
    if (!applicationId || !clientToken) {
      return;
    }

    const config: RumInitConfiguration = {
      applicationId,
      clientToken,
      site: (getConfigValue("DD_SITE") ?? "datadoghq.com") as RumInitConfiguration["site"],
      service: getConfigValue("DD_SERVICE") ?? context.service,
      env: getConfigValue("DD_ENV") ?? context.env,
      version: context.version,
      sessionSampleRate: parseSampleRate(getConfigValue("DD_RUM_SAMPLE_RATE"), 100),
      sessionReplaySampleRate: parseSampleRate(getConfigValue("DD_SESSION_REPLAY_SAMPLE_RATE"), 0),
      traceSampleRate: parseSampleRate(getConfigValue("DD_TRACE_SAMPLE_RATE"), 100),
      trackResources: true,
      trackLongTasks: true,
      trackUserInteractions: true,
      allowedTracingUrls: [makeApiTracingMatcher(context.apiTracingPathPrefix)],
    };

    datadogRum.init(config);
  },

  reportError(error: unknown, meta?: ObservabilityErrorMeta): void {
    datadogRum.addError(error, meta);
  },
};
