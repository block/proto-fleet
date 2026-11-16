import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { alarmActionSupport, plantAlarmItems } from "./plantAlarms";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import {
  ComponentType,
  ErrorMessageSchema,
  MinerError,
  Severity,
} from "@/protoFleet/api/generated/errors/v1/errors_pb";

const seen = new Date("2026-11-14T10:00:00Z");
// Shaped like the CT1 plugin's plant alarm reports.
const plantError = (id: string, overrides: Record<string, unknown> = {}) =>
  create(ErrorMessageSchema, {
    errorId: `err-${id}`,
    canonicalError: MinerError.VENDOR_ERROR_UNMAPPED,
    summary: `Plant alarm: ${id} (component pump-1)`,
    causeSummary: `${id} (component pump-1)`,
    severity: Severity.MAJOR,
    lastSeenAt: timestampFromDate(seen),
    deviceIdentifier: "ct1",
    componentId: `plant-alarm:${id}`,
    componentType: ComponentType.UNSPECIFIED,
    vendorAttributes: { vendor_code: id, ct1_evidence: "current", plant_alarm_severity: "SEVERITY_ERROR" },
    ...overrides,
  });

describe("plantAlarmItems", () => {
  it("keeps only plant alarm components, most severe first, and marks carried alarms", () => {
    const items = plantAlarmItems([
      plantError("COOLANT_LOW", { severity: Severity.MINOR }),
      plantError("FIRE", { severity: Severity.CRITICAL }),
      plantError("PUMP_FAULT", {
        vendorAttributes: { ct1_evidence: "retained", ct1_last_confirmed_at: "2026-11-14T09:30:00Z" },
      }),
      plantError("hashboard", { componentId: "mac:aa:bb", severity: Severity.CRITICAL }),
      plantError("plant", { componentId: "plant" }),
      plantError("CLOSED", { closedAt: timestampFromDate(seen) }),
    ]);
    expect(items.map((item) => item.name)).toEqual([
      "FIRE (component pump-1)",
      "PUMP_FAULT (component pump-1)",
      "COOLANT_LOW (component pump-1)",
    ]);
    expect(items.map((item) => item.retained)).toEqual([false, true, false]);
    expect(items[1].confirmedAt).toEqual(new Date("2026-11-14T09:30:00Z"));
    expect(items[0].confirmedAt).toEqual(seen);
  });

  it("falls back to the alarm id when no text was reported", () => {
    const [item] = plantAlarmItems([plantError("PUMP_FAULT", { causeSummary: "", summary: "" })]);
    expect(item.name).toBe("PUMP_FAULT");
  });
});

describe("alarmActionSupport", () => {
  const withControls = (controls: Record<string, unknown>) =>
    create(ContainerSchema, { controls: { supported: true, observedAt: timestampFromDate(new Date()), ...controls } });
  it("needs a fresh observation that advertises the action", () => {
    expect(alarmActionSupport(withControls({ alarmMuteSupported: true }), "mute")).toBe("supported");
    expect(alarmActionSupport(withControls({ alarmResetSupported: false }), "reset")).toBe("unsupported");
    expect(alarmActionSupport(withControls({}), "reset")).toBe("unknown");
    expect(alarmActionSupport(withControls({ alarmMuteSupported: true, stale: true }), "mute")).toBe("unknown");
    expect(
      alarmActionSupport(
        withControls({ alarmMuteSupported: true, observedAt: timestampFromDate(new Date(Date.now() - 180_000)) }),
        "mute",
      ),
    ).toBe("unknown");
    expect(alarmActionSupport(undefined, "mute")).toBe("unknown");
  });
});
