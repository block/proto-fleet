import { describe, expect, it } from "vitest";
import { type FanStatusInput, toFanComponentStatus } from "./fanStatusModal";

const runningFan: FanStatusInput = { label: "Fan 3", on: true, speedPercent: 68, powerLabel: "3,200" };

describe("toFanComponentStatus", () => {
  it("maps a running fan to the shared fan component type with normal summary", () => {
    const status = toFanComponentStatus(runningFan);

    expect(status.componentType).toBe("fan");
    expect(status.summary).toBe("Fan 3 is operating normally");
    expect(status.errors).toEqual([]);
  });

  it("mirrors the card footer's W and speed readouts as metrics", () => {
    const status = toFanComponentStatus(runningFan);

    expect(status.metrics).toEqual([
      { label: "Power", value: "3,200 W" },
      { label: "Speed", value: "68%" },
    ]);
    expect(status.metadata?.status).toEqual({ label: "Status", value: "Running" });
  });

  it("zeroes the readouts and reports Off for a powered-off fan", () => {
    const status = toFanComponentStatus({ label: "Fan 4", on: false, speedPercent: 0, powerLabel: "0" });

    expect(status.summary).toBe("Fan 4 is powered off");
    expect(status.metrics).toEqual([
      { label: "Power", value: "0 W" },
      { label: "Speed", value: "0%" },
    ]);
    expect(status.metadata?.status).toEqual({ label: "Status", value: "Off" });
  });

  it("rounds and clamps the speed percentage into 0–100", () => {
    expect(
      toFanComponentStatus({ label: "F", on: true, speedPercent: 66.7, powerLabel: "3,100" }).metrics?.[1],
    ).toEqual({ label: "Speed", value: "67%" });
    expect(toFanComponentStatus({ label: "F", on: true, speedPercent: 140, powerLabel: "x" }).metrics?.[1]).toEqual({
      label: "Speed",
      value: "100%",
    });
    expect(toFanComponentStatus({ label: "F", on: true, speedPercent: -5, powerLabel: "x" }).metrics?.[1]).toEqual({
      label: "Speed",
      value: "0%",
    });
  });
});
