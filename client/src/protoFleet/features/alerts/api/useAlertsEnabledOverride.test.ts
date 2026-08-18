import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { _resetAlertsEnabledCache, useAlertsEnabled } from "@/protoFleet/features/alerts/api/useAlertsEnabled";

// The build-time override is read once at module scope, so forcing it on needs its own file.
vi.mock("@/protoFleet/constants/featureFlags", () => ({ ALERTS_ENABLED: true }));

describe("useAlertsEnabled with the build-time override on", () => {
  beforeEach(() => {
    _resetAlertsEnabledCache();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("keeps alerts on across a remount that reads a cached server 'disabled'", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, json: async () => ({ enabled: false }) } as unknown as Response),
    );

    const first = renderHook(() => useAlertsEnabled());
    await waitFor(() => expect(first.result.current).toBe(true));
    first.unmount();

    // The cached answer short-circuits the probe, so the override has to survive the initializer alone.
    const second = renderHook(() => useAlertsEnabled());
    expect(second.result.current).toBe(true);
  });
});
