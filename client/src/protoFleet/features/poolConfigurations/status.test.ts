// @vitest-environment node
import { describe, expect, it } from "vitest";
import { poolStatusExplanation, poolStatusIsProblem, poolUpdateStatus } from "./status";

describe("pool configuration status copy", () => {
  it.each([
    ["superseded", "write_uncertain", "Replaced by a newer update"],
    ["superseded", "interrupted", "Replaced by a newer update"],
    ["pending", "", "Updating…"],
    ["applying", "", "Updating…"],
    ["verified", "", "Verified"],
    ["recoverable", "", "Recovery available"],
    ["placement_pending", "", "Assignment pending"],
    ["adopted", "", "Not verified"],
    ["applied_unverified", "readback_unavailable", "Not verified"],
    ["failed", "write_uncertain", "Not verified"],
    ["failed", "unavailable", "Update failed"],
    ["failed", "interrupted", "Update failed"],
    ["different", "settings_differ", "Settings differ"],
    ["different", "write_uncertain", "Settings differ"],
    ["device_configuration", "", "Device configuration"],
    ["container_configuration", "", "Container configuration"],
    ["cleared", "", "—"],
    ["", "", "—"],
    ["unexpected", "", "Unavailable"],
  ])("maps %s / %s to %s", (state, resultCode, expected) => {
    expect(poolUpdateStatus({ state, resultCode })).toBe(expected);
  });
  it("distinguishes problem statuses from other configuration states", () => {
    for (const status of ["Verified", "Device configuration", "Updating…", "—", "Mixed configurations", "Loading…"])
      expect(poolStatusIsProblem(status)).toBe(false);
    for (const status of [
      "Update failed",
      "Not verified",
      "Settings differ",
      "Unavailable",
      "Needs attention",
      "Recovery available",
      "Assignment pending",
    ])
      expect(poolStatusIsProblem(status)).toBe(true);
  });
  it("distinguishes uncertain writes from accepted but unreadable updates", () => {
    expect(poolStatusExplanation({ state: "failed", resultCode: "write_uncertain" })).toBe(
      "Fleet couldn’t confirm that an earlier device update finished.",
    );
    expect(poolStatusExplanation({ state: "applied_unverified", resultCode: "readback_unavailable" })).toBe(
      "The equipment accepted the update, but Fleet could not verify its pool settings.",
    );
  });
});

it.each(["write_uncertain", "interrupted", "check_unavailable"])(
  "explains supersession before the old %s result",
  (resultCode) => {
    expect(poolStatusExplanation({ state: "superseded", resultCode })).toBe(
      "A newer pool configuration update replaced this update.",
    );
  },
);

it("explains recovery without implying a controller write is running", () => {
  const explanation = poolStatusExplanation({ state: "recoverable", resultCode: "" });
  expect(explanation).toContain("Keep device settings");
  expect(explanation).toContain("Remove assignment");
  expect(explanation).not.toContain("may still be running");
});

it("explains that placement recovery blocks only the affected board", () => {
  const explanation = poolStatusExplanation({ state: "placement_pending", resultCode: "" });
  expect(explanation).toContain("Other mapped boards can be updated");
  expect(explanation).toContain("location or ownership");
});
