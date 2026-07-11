import { datadogRum } from "@datadog/browser-rum";
import type { RumEvent, RumInitConfiguration } from "@datadog/browser-rum";

import { getConfigValue } from "../runtimeConfig";
import type { ObservabilityErrorMeta, ObservabilityInitContext, ObservabilityProvider } from "../types";

/** Required config keys — the provider is a no-op unless both are present. */
const REQUIRED_KEYS = ["DD_APPLICATION_ID", "DD_CLIENT_TOKEN"] as const;

/** Parse a sample rate, clamped to the documented 0-100 range so a
 * misconfigured value (e.g. 1000 or -1) can't produce unexpected sampling. */
const parseSampleRate = (value: string | undefined, fallback: number): number => {
  if (value === undefined) {
    return fallback;
  }
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) {
    return fallback;
  }
  return Math.min(100, Math.max(0, parsed));
};

/** Drop the query string (and fragment) from a URL, keeping origin + path.
 * Query params on fleet API/resource URLs can carry identifiers or filters we
 * don't want leaving the browser. Returns the input unchanged if unparseable. */
const stripUrlQuery = (url: string): string => {
  try {
    const parsed = new URL(url, window.location.origin);
    return `${parsed.origin}${parsed.pathname}`;
  } catch {
    return url;
  }
};

/** beforeSend scrubber: redact query strings from URL fields before events
 * leave the browser. Every event carries the active view URL, and ProtoFleet
 * stores fleet context (group/rack/building IDs, subnet filters) in the query
 * string, so the view URL/referrer are scrubbed on all event types, plus the
 * resource URL on resource events. Extension point for further redaction
 * (action names, error metadata) if the UI surfaces more sensitive strings. */
const scrubEvent = (event: RumEvent): boolean => {
  event.view.url = stripUrlQuery(event.view.url);
  if (event.view.referrer) {
    event.view.referrer = stripUrlQuery(event.view.referrer);
  }
  if (event.type === "resource" && event.resource.url) {
    event.resource.url = stripUrlQuery(event.resource.url);
  }
  return true;
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
      // Session Replay is off by default (rate 0); when an operator enables it,
      // mask all text and inputs so rendered fleet data isn't recorded verbatim.
      sessionReplaySampleRate: parseSampleRate(getConfigValue("DD_SESSION_REPLAY_SAMPLE_RATE"), 0),
      defaultPrivacyLevel: "mask",
      traceSampleRate: parseSampleRate(getConfigValue("DD_TRACE_SAMPLE_RATE"), 100),
      trackResources: true,
      trackLongTasks: true,
      trackUserInteractions: true,
      allowedTracingUrls: [makeApiTracingMatcher(context.apiTracingPathPrefix)],
      beforeSend: scrubEvent,
    };

    datadogRum.init(config);
  },

  reportError(error: unknown, meta?: ObservabilityErrorMeta): void {
    datadogRum.addError(error, meta);
  },
};
