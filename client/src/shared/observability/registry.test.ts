import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  __resetObservabilityForTests,
  initObservability,
  observabilityInterceptors,
  registerProvider,
  reportObservabilityError,
} from "./registry";
import type { ObservabilityInitContext, ObservabilityProvider } from "./types";

const ctx: ObservabilityInitContext = {
  service: "test-service",
  version: "test-commit",
  env: "test",
  apiTracingOrigin: "/api-proxy",
};

beforeEach(() => {
  __resetObservabilityForTests();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("initObservability", () => {
  it("initializes a configured provider once with the context", () => {
    const init = vi.fn();
    registerProvider({ name: "configured", isConfigured: () => true, init });

    initObservability(ctx);

    expect(init).toHaveBeenCalledTimes(1);
    expect(init).toHaveBeenCalledWith(ctx);
  });

  it("skips an unconfigured provider", () => {
    const init = vi.fn();
    registerProvider({ name: "unconfigured", isConfigured: () => false, init });

    initObservability(ctx);

    expect(init).not.toHaveBeenCalled();
  });

  it("isolates a provider init failure so other providers still initialize", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const goodInit = vi.fn();
    registerProvider({
      name: "throwing",
      isConfigured: () => true,
      init: () => {
        throw new Error("boom");
      },
    });
    registerProvider({ name: "good", isConfigured: () => true, init: goodInit });

    expect(() => initObservability(ctx)).not.toThrow();
    expect(goodInit).toHaveBeenCalledTimes(1);
    expect(consoleError).toHaveBeenCalled();
  });

  it("is idempotent: a second call does not re-initialize providers", () => {
    const init = vi.fn();
    registerProvider({ name: "configured", isConfigured: () => true, init });

    initObservability(ctx);
    initObservability(ctx);

    expect(init).toHaveBeenCalledTimes(1);
  });
});

describe("registerProvider", () => {
  it("does not register the same provider name twice", () => {
    const initA = vi.fn();
    const initB = vi.fn();
    registerProvider({ name: "dup", isConfigured: () => true, init: initA });
    registerProvider({ name: "dup", isConfigured: () => true, init: initB });

    initObservability(ctx);

    expect(initA).toHaveBeenCalledTimes(1);
    expect(initB).not.toHaveBeenCalled();
  });
});

describe("reportObservabilityError", () => {
  it("forwards only to configured providers that implement reportError", () => {
    const configuredReport = vi.fn();
    const unconfiguredReport = vi.fn();
    registerProvider({
      name: "configured",
      isConfigured: () => true,
      init: vi.fn(),
      reportError: configuredReport,
    });
    registerProvider({
      name: "unconfigured",
      isConfigured: () => false,
      init: vi.fn(),
      reportError: unconfiguredReport,
    });

    const error = new Error("render failed");
    reportObservabilityError(error, { react: "info" });

    expect(configuredReport).toHaveBeenCalledWith(error, { react: "info" });
    expect(unconfiguredReport).not.toHaveBeenCalled();
  });

  it("never throws when a provider's reportError throws", () => {
    registerProvider({
      name: "throwing",
      isConfigured: () => true,
      init: vi.fn(),
      reportError: () => {
        throw new Error("report boom");
      },
    });

    expect(() => reportObservabilityError(new Error("x"))).not.toThrow();
  });
});

describe("observabilityInterceptors", () => {
  it("collects interceptors only from configured providers", () => {
    const interceptor = ((next) => next) as ReturnType<
      NonNullable<ObservabilityProvider["connectInterceptors"]>
    >[number];
    registerProvider({
      name: "configured",
      isConfigured: () => true,
      init: vi.fn(),
      connectInterceptors: () => [interceptor],
    });
    registerProvider({
      name: "unconfigured",
      isConfigured: () => false,
      init: vi.fn(),
      connectInterceptors: () => [interceptor],
    });

    expect(observabilityInterceptors()).toEqual([interceptor]);
  });

  it("returns an empty array when no provider contributes interceptors", () => {
    registerProvider({ name: "plain", isConfigured: () => true, init: vi.fn() });
    expect(observabilityInterceptors()).toEqual([]);
  });
});
