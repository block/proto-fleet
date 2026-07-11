import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { datadogRum } from "@datadog/browser-rum";
import type { RumInitConfiguration } from "@datadog/browser-rum";

import type { ObservabilityInitContext } from "../types";
import { datadogProvider } from "./datadog";

const ctx: ObservabilityInitContext = {
  service: "proto-fleet-client",
  version: "test-commit",
  env: "test",
  apiTracingPathPrefix: "/api-proxy",
};

const setRuntimeConfig = (config: Record<string, string>) => {
  window.__RUNTIME_CONFIG__ = config;
};

const initMock = vi.mocked(datadogRum.init);
const addErrorMock = vi.mocked(datadogRum.addError);

const lastInitConfig = (): RumInitConfiguration => initMock.mock.calls[0][0];

beforeEach(() => {
  delete window.__RUNTIME_CONFIG__;
  initMock.mockClear();
  addErrorMock.mockClear();
});

afterEach(() => {
  vi.unstubAllEnvs();
});

describe("datadogProvider.isConfigured", () => {
  it("is false when either required key is missing", () => {
    setRuntimeConfig({ DD_APPLICATION_ID: "app-id" });
    expect(datadogProvider.isConfigured()).toBe(false);

    setRuntimeConfig({ DD_CLIENT_TOKEN: "token" });
    expect(datadogProvider.isConfigured()).toBe(false);
  });

  it("is true when both required keys are present", () => {
    setRuntimeConfig({ DD_APPLICATION_ID: "app-id", DD_CLIENT_TOKEN: "token" });
    expect(datadogProvider.isConfigured()).toBe(true);
  });

  it("reads from build-time VITE_ vars as a fallback", () => {
    vi.stubEnv("VITE_DD_APPLICATION_ID", "app-id");
    vi.stubEnv("VITE_DD_CLIENT_TOKEN", "token");
    expect(datadogProvider.isConfigured()).toBe(true);
  });
});

describe("datadogProvider.init", () => {
  it("does not initialize RUM when required keys are missing", () => {
    datadogProvider.init(ctx);
    expect(initMock).not.toHaveBeenCalled();
  });

  it("initializes RUM once with required config and version from the context", () => {
    setRuntimeConfig({ DD_APPLICATION_ID: "app-id", DD_CLIENT_TOKEN: "token" });

    datadogProvider.init(ctx);

    expect(initMock).toHaveBeenCalledTimes(1);
    const config = lastInitConfig();
    expect(config.applicationId).toBe("app-id");
    expect(config.clientToken).toBe("token");
    expect(config.version).toBe("test-commit");
  });

  it("applies defaults for optional keys and honors overrides", () => {
    setRuntimeConfig({ DD_APPLICATION_ID: "app-id", DD_CLIENT_TOKEN: "token" });
    datadogProvider.init(ctx);
    let config = lastInitConfig();
    expect(config.site).toBe("datadoghq.com");
    expect(config.service).toBe("proto-fleet-client");
    expect(config.env).toBe("test"); // falls back to context.env
    expect(config.sessionSampleRate).toBe(100);
    expect(config.sessionReplaySampleRate).toBe(0);
    expect(config.traceSampleRate).toBe(100);

    initMock.mockClear();
    setRuntimeConfig({
      DD_APPLICATION_ID: "app-id",
      DD_CLIENT_TOKEN: "token",
      DD_SITE: "us3.datadoghq.com",
      DD_SERVICE: "custom-service",
      DD_ENV: "staging",
      DD_RUM_SAMPLE_RATE: "25",
      DD_SESSION_REPLAY_SAMPLE_RATE: "10",
      DD_TRACE_SAMPLE_RATE: "50",
    });
    datadogProvider.init(ctx);
    config = lastInitConfig();
    expect(config.site).toBe("us3.datadoghq.com");
    expect(config.service).toBe("custom-service");
    expect(config.env).toBe("staging"); // DD_ENV overrides context.env
    expect(config.sessionSampleRate).toBe(25);
    expect(config.sessionReplaySampleRate).toBe(10);
    expect(config.traceSampleRate).toBe(50);
  });

  it("falls back to defaults when a sample rate is non-numeric", () => {
    setRuntimeConfig({
      DD_APPLICATION_ID: "app-id",
      DD_CLIENT_TOKEN: "token",
      DD_RUM_SAMPLE_RATE: "not-a-number",
    });
    datadogProvider.init(ctx);
    expect(lastInitConfig().sessionSampleRate).toBe(100);
  });

  it("clamps out-of-range sample rates to 0-100", () => {
    setRuntimeConfig({
      DD_APPLICATION_ID: "app-id",
      DD_CLIENT_TOKEN: "token",
      DD_RUM_SAMPLE_RATE: "1000",
      DD_TRACE_SAMPLE_RATE: "-5",
    });
    datadogProvider.init(ctx);
    const config = lastInitConfig();
    expect(config.sessionSampleRate).toBe(100);
    expect(config.traceSampleRate).toBe(0);
  });

  it("masks replay content and scrubs query strings from view and resource URLs", () => {
    setRuntimeConfig({ DD_APPLICATION_ID: "app-id", DD_CLIENT_TOKEN: "token" });
    datadogProvider.init(ctx);
    const config = lastInitConfig();

    expect(config.defaultPrivacyLevel).toBe("mask");

    const origin = window.location.origin;
    const beforeSend = config.beforeSend as (event: {
      type: string;
      view: { url: string; referrer?: string };
      resource?: { url: string };
    }) => boolean;

    // Resource event: view URL, referrer, and resource URL are all scrubbed.
    const resourceEvent = {
      type: "resource",
      view: { url: `${origin}/fleet/miners?subnet=10.0.0.0/24&rackId=r1`, referrer: `${origin}/x?token=t` },
      resource: { url: `${origin}/api-proxy/svc/Method?minerId=abc123&token=secret` },
    };
    expect(beforeSend(resourceEvent)).toBe(true);
    expect(resourceEvent.view.url).toBe(`${origin}/fleet/miners`);
    expect(resourceEvent.view.referrer).toBe(`${origin}/x`);
    expect(resourceEvent.resource?.url).toBe(`${origin}/api-proxy/svc/Method`);

    // Non-resource (view) event: the view URL is still scrubbed.
    const viewEvent = { type: "view", view: { url: `${origin}/fleet/miners?buildingId=b2` } };
    expect(beforeSend(viewEvent)).toBe(true);
    expect(viewEvent.view.url).toBe(`${origin}/fleet/miners`);
  });

  it("scopes distributed tracing to same-origin API calls only", () => {
    setRuntimeConfig({ DD_APPLICATION_ID: "app-id", DD_CLIENT_TOKEN: "token" });
    datadogProvider.init(ctx);

    const tracing = lastInitConfig().allowedTracingUrls ?? [];
    const matcher = tracing[0] as (url: string) => boolean;
    const origin = window.location.origin;

    expect(matcher(`${origin}/api-proxy/telemetry.v1.TelemetryService/Get`)).toBe(true);
    expect(matcher(`${origin}/other/path`)).toBe(false);
    expect(matcher(`${origin}/api-proxy-internal/x`)).toBe(false); // sibling path not swept in
    expect(matcher("https://third-party.example.com/api-proxy/x")).toBe(false);
  });
});

describe("datadogProvider.reportError", () => {
  it("forwards errors to datadogRum.addError", () => {
    const error = new Error("render failed");
    datadogProvider.reportError?.(error, { react: "info" });
    expect(addErrorMock).toHaveBeenCalledWith(error, { react: "info" });
  });
});
