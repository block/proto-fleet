import { expect, test } from "vitest";
import { moduleActionTarget } from "./moduleActions";
import { buildModuleRows } from "./moduleListModel";
import { tanksFixture } from "@/protoOS/features/tanks/Tanks.fixtures";
import { asComponentId } from "@/protoOS/store/contract";

test("targets only the selected modules' boards across repeated tank positions", () => {
  const { topology, telemetry } = tanksFixture(2, 2);
  const rows = buildModuleRows(topology, telemetry);
  const target = moduleActionTarget(rows, [rows[0].id, rows[2].id, rows[0].id], topology)!;
  expect(target.names).toEqual(["T1-P1", "T2-P1"]);
  expect(target.identifiers).toEqual(
    [rows[0], rows[2]]
      .flatMap((row) => row.module.hashboardIds.map((id) => topology.hashboards.get(id)!.identity.value))
      .sort(),
  );
  expect(target.identifiers).toHaveLength(6);
  expect(moduleActionTarget(rows, [], topology)).toBeUndefined();
  expect(moduleActionTarget(rows, ["missing"], topology)).toBeUndefined();
  topology.hashboards.delete(rows[0].module.hashboardIds[0]);
  expect(moduleActionTarget(rows, [rows[0].id], topology)).toBeUndefined();
});

test("module error indicators include PSU errors without marking unrelated modules", () => {
  const { topology, telemetry } = tanksFixture(2, 1);
  const module = [...topology.modules.values()][0];
  const psuId = asComponentId("psu:test");
  module.psuIds.push(psuId);
  for (const reading of telemetry.hashboards.values()) {
    reading.health = "ok";
    reading.reportedIssueCount = 0;
  }
  const rows = buildModuleRows(topology, telemetry, [
    {
      id: "psu-error",
      code: "overtemp",
      message: "PSU is too hot",
      severity: "critical",
      owner: { kind: "module", id: module.id },
      component: { kind: "psu", id: psuId, owner: { kind: "module", id: module.id } },
    },
  ]);
  expect(rows.map((row) => row.hasErrors)).toEqual([true, false]);
  telemetry.psus.set(psuId, { componentId: psuId, health: "fault" });
  expect(buildModuleRows(topology, telemetry).map((row) => row.hasErrors)).toEqual([true, false]);
});
