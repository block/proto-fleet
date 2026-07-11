import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { getConfigValue } from "./runtimeConfig";

type MutableWindow = Window & { __RUNTIME_CONFIG__?: Record<string, string> };

const clearRuntimeConfig = () => {
  delete (window as MutableWindow).__RUNTIME_CONFIG__;
};

describe("getConfigValue", () => {
  beforeEach(clearRuntimeConfig);

  afterEach(() => {
    vi.unstubAllEnvs();
    clearRuntimeConfig();
  });

  it("returns the runtime value when present", () => {
    (window as MutableWindow).__RUNTIME_CONFIG__ = { DD_CLIENT_TOKEN: "runtime-token" };
    expect(getConfigValue("DD_CLIENT_TOKEN")).toBe("runtime-token");
  });

  it("falls back to the build-time VITE_ value when there is no runtime value", () => {
    vi.stubEnv("VITE_DD_CLIENT_TOKEN", "build-token");
    expect(getConfigValue("DD_CLIENT_TOKEN")).toBe("build-token");
  });

  it("prefers the runtime value over the build-time value", () => {
    vi.stubEnv("VITE_DD_CLIENT_TOKEN", "build-token");
    (window as MutableWindow).__RUNTIME_CONFIG__ = { DD_CLIENT_TOKEN: "runtime-token" };
    expect(getConfigValue("DD_CLIENT_TOKEN")).toBe("runtime-token");
  });

  it("treats an empty or whitespace runtime value as unset and falls back", () => {
    vi.stubEnv("VITE_DD_CLIENT_TOKEN", "build-token");
    (window as MutableWindow).__RUNTIME_CONFIG__ = { DD_CLIENT_TOKEN: "   " };
    expect(getConfigValue("DD_CLIENT_TOKEN")).toBe("build-token");
  });

  it("returns undefined when neither source is set", () => {
    expect(getConfigValue("DD_CLIENT_TOKEN")).toBeUndefined();
  });
});
