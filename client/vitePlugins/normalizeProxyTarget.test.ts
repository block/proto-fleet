import { describe, expect, test } from "vitest";

import { normalizeProxyTarget } from "./normalizeProxyTarget";

describe("normalizeProxyTarget", () => {
  test("adds http:// to a bare miner IP", () => {
    expect(normalizeProxyTarget("172.16.21.119")).toBe("http://172.16.21.119");
  });

  test("preserves explicit HTTP and HTTPS targets", () => {
    expect(normalizeProxyTarget("http://127.0.0.1:8080")).toBe("http://127.0.0.1:8080");
    expect(normalizeProxyTarget("https://miner.example.test")).toBe("https://miner.example.test");
  });

  test("rejects non-HTTP proxy protocols", () => {
    expect(() => normalizeProxyTarget("ftp://miner.example.test")).toThrow("Proxy target must use http:// or https://");
  });
});
