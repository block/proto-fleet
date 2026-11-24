import { act, renderHook } from "@testing-library/react";
import { beforeEach, expect, test } from "vitest";
import { useMinerStatusTitle } from "./useMinerStatusTitle";
import { toCanonicalErrors } from "@/protoOS/api/adapters/mdkV2/normalize";
import { transformErrorsForModal } from "@/protoOS/components/StatusModal/utils";
import { asModuleId, type MinerError } from "@/protoOS/store/contract";
import { isPlantAlarmError, useDetailedErrors, useGroupedErrors } from "@/protoOS/store/hooks/useMinerStatus";
import useMinerStore from "@/protoOS/store/useMinerStore";

// Generated MDK enums are private to the adapter; their values are the wire strings.
type Severity = NonNullable<
  NonNullable<NonNullable<Parameters<typeof toCanonicalErrors>[2]>["alarms"]>[number]["severity"]
>;

const plantAlarms = () =>
  toCanonicalErrors({}, "ct1", {
    alarms: [
      { id: "COOLANT_LOW", name: "Coolant low pressure", severity: "SEVERITY_WARNING" as Severity },
      { id: "TANK_B_HOT", name: "Tank B outlet over temperature", severity: "SEVERITY_CRITICAL" as Severity },
    ],
  });

const hashboardFault: MinerError = {
  id: "hashboard-fault",
  code: "hashboard-fault",
  message: "Hashboard fault",
  severity: "critical",
  owner: { kind: "module", id: asModuleId("module:1") },
  source: { kind: "hashboard", slot: 1 },
};

const rigFault: MinerError = {
  id: "rig-fault",
  code: "rig-fault",
  message: "Control board fault",
  severity: "critical",
  owner: { kind: "module", id: asModuleId("module:1") },
  source: { kind: "rig" },
};

beforeEach(() => useMinerStore.getState().resetDeviceData());

test("counts plant alarms as issues instead of control board failures", () => {
  act(() => useMinerStore.getState().minerStatus.setErrors(plantAlarms()));
  const grouped = renderHook(() => useGroupedErrors()).result.current;
  expect(grouped.system).toEqual([]);
  expect(grouped.plant).toHaveLength(2);
  expect(renderHook(() => useMinerStatusTitle()).result.current.title).toBe("2 issues");
});

test("plant alarms alongside a hardware fault read as multiple failures", () => {
  act(() => useMinerStore.getState().minerStatus.setErrors([...plantAlarms(), hashboardFault]));
  expect(renderHook(() => useMinerStatusTitle()).result.current.title).toBe("Multiple failures");
});

test("rig errors without a component still count as control board failures", () => {
  act(() => useMinerStore.getState().minerStatus.setErrors([rigFault]));
  const grouped = renderHook(() => useGroupedErrors()).result.current;
  expect(grouped.system).toHaveLength(1);
  expect(grouped.plant).toEqual([]);
  expect(isPlantAlarmError(rigFault)).toBe(false);
});

test("labels plant alarm rows in the status modal", () => {
  act(() => useMinerStore.getState().minerStatus.setErrors(plantAlarms()));
  const errors = renderHook(() => useDetailedErrors()).result.current;
  expect(transformErrorsForModal(errors, () => "unused").map((row) => [row.componentName, row.message])).toEqual([
    ["Container alarm", "Warning: Coolant low pressure"],
    ["Container alarm", "Critical: Tank B outlet over temperature"],
  ]);
});
