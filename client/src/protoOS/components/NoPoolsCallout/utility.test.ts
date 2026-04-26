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

  it.each(["/settings/mining-pools/extra", "/onboarding/mining-pool", "/groups/1/settings/mining-pools"])(
    "shows the callout on non-mining-pools null state path %s",
    (pathname) => {
      expect(getNoPoolsCalloutState([], pathname)).toEqual({
        arePoolsConfigured: false,
        noPoolsLive: true,
        shouldShowNoPoolsCallout: true,
      });
    },
  );

  it("shows the callout on the mining pools page when configured pools are inactive", () => {
    expect(
      getNoPoolsCalloutState(
        [{ status: "Dead" }, { status: "Dead", url: "stratum+tcp://backup.example:3333" }],
        "/miners/miner-1/settings/mining-pools",
      ),
    ).toEqual({
      arePoolsConfigured: true,
      noPoolsLive: true,
      shouldShowNoPoolsCallout: true,
    });
  });
});
