import { describe, expect, it } from "vitest";
import { getUpgradeProgressCopy } from "./upgradeProgressCopy";
import { UpgradePhase } from "@/protoFleet/api/generated/instance/v1/updates_pb";

describe("getUpgradeProgressCopy", () => {
  it.each([
    [UpgradePhase.QUEUED, "Preparing update"],
    [UpgradePhase.STAGING, "Preparing update"],
    [UpgradePhase.DOWNLOADING, "Downloading update"],
    [UpgradePhase.VERIFYING, "Checking update"],
    [UpgradePhase.PREFLIGHT, "Checking update"],
    [UpgradePhase.ACTIVATING, "Restarting Fleet"],
    [UpgradePhase.UNSPECIFIED, "Updating Fleet"],
  ])("uses user-facing copy for phase %s", (phase, expected) => {
    expect(getUpgradeProgressCopy(phase)).toBe(expected);
  });
});
