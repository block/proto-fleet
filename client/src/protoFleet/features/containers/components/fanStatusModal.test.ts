import { describe, expect, it } from "vitest";
import { type FanStatusInput, toFanComponentStatus } from "./fanStatusModal";

const runningFan: FanStatusInput = { label: "Fan 3", on: true, speedPercent: 68, speedLabel: "3,200" };

describe("toFanComponentStatus", () => {
  it("maps a running fan to the shared fan component type with normal summary", () => {
    const status = toFanComponentStatus(runningFan);

    expect(status.componentType).toBe("fan");
    expect(status.summary).toBe("Fan 3 is operating normally");
    expect(status.errors).toEqual([]);
  });

  it("mirrors the card footer's RPM and PWM readouts as metrics", () => {
    const status = toFanComponentStatus(runningFan);

    expect(status.metrics).toEqual([
      { label: "Speed", value: "3,200 RPM" },
      { label: "PWM", value: "68%" },
    ]);
    expect(status.metadata?.status).toEqual({ label: "Status", value: "Running" });
  });

  it("zeroes the readouts and reports Off for a powered-off fan", () => {
    const status = toFanComponentStatus({ label: "Fan 4", on: false, speedPercent: 0, speedLabel: "0" });

    expect(status.summary).toBe("Fan 4 is powered off");
    expect(status.metrics).toEqual([
      { label: "Speed", value: "0 RPM" },
      { label: "PWM", value: "0%" },
    ]);
    expect(status.metadata?.status).toEqual({ label: "Status", value: "Off" });
  });

  it("rounds and clamps the PWM percentage into 0–100", () => {
    expect(
      toFanComponentStatus({ label: "F", on: true, speedPercent: 66.7, speedLabel: "3,100" }).metrics?.[1],
    ).toEqual({ label: "PWM", value: "67%" });
    expect(toFanComponentStatus({ label: "F", on: true, speedPercent: 140, speedLabel: "x" }).metrics?.[1]).toEqual({
      label: "PWM",
      value: "100%",
    });
    expect(toFanComponentStatus({ label: "F", on: true, speedPercent: -5, speedLabel: "x" }).metrics?.[1]).toEqual({
      label: "PWM",
      value: "0%",
    });
  });
});
