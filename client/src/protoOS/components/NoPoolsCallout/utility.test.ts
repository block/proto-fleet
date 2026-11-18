import { describe, expect, it } from "vitest";

import { getNoPoolsCalloutState } from "./utility";

describe("NoPoolsCallout utility", () => {
  it("does not show the callout while pool data is still loading", () => {
    expect(getNoPoolsCalloutState(undefined, "/settings/general")).toEqual({
      arePoolsConfigured: false,
      noPoolsLive: false,
      shouldShowNoPoolsCallout: false,
    });
  });

  it("does not show the callout when a pool is live", () => {
    expect(getNoPoolsCalloutState([{ status: "Dead" }, { status: "Active" }], "/settings/general")).toEqual({
      arePoolsConfigured: false,
      noPoolsLive: false,
      shouldShowNoPoolsCallout: false,
    });
  });

  it.each([
    "/pools",
    "/pools/",
    "/miners/miner-1/pools",
    "/miners/miner%2F1/pools/",
    "/POOLS",
    "/settings/mining-pools",
    "/settings/mining-pools/",
    "/miners/miner-1/settings/mining-pools",
    "/miners/miner-1/settings/mining-pools/",
    "/miners/miner%2F1/settings/mining-pools/",
    "/SETTINGS/MINING-POOLS",
  ])("hides the callout on the mining pools null state for %s", (pathname) => {
    expect(getNoPoolsCalloutState([], pathname)).toEqual({
      arePoolsConfigured: false,
      noPoolsLive: true,
      shouldShowNoPoolsCallout: false,
    });
  });

  it.each([
    "/pools/extra",
    "/groups/1/pools",
    "/settings/mining-pools/extra",
    "/onboarding/mining-pool",
    "/groups/1/settings/mining-pools",
  ])("shows the callout on non-mining-pools null state path %s", (pathname) => {
    expect(getNoPoolsCalloutState([], pathname)).toEqual({
      arePoolsConfigured: false,
      noPoolsLive: true,
      shouldShowNoPoolsCallout: true,
    });
  });

  it.each(["/pools", "/miners/miner-1/pools", "/miners/miner-1/settings/mining-pools"])(
    "shows the callout on %s when configured pools are inactive",
    (pathname) => {
      expect(
        getNoPoolsCalloutState(
          [{ status: "Dead" }, { status: "Dead", url: "stratum+tcp://backup.example:3333" }],
          pathname,
        ),
      ).toEqual({
        arePoolsConfigured: true,
        noPoolsLive: true,
        shouldShowNoPoolsCallout: true,
      });
    },
  );
  it("suppresses only lost connections while mapping runs", () => {
    const pools = [{ status: "Idle", url: "stratum+tcp://pool.example:3333" }];
    expect(getNoPoolsCalloutState(pools, "/", true).shouldShowNoPoolsCallout).toBe(false);
    expect(getNoPoolsCalloutState([], "/", true).shouldShowNoPoolsCallout).toBe(true);
    expect(getNoPoolsCalloutState(pools, "/", false).shouldShowNoPoolsCallout).toBe(true);
  });
});
