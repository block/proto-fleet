import { describe, expect, test } from "vitest";

import { buildModuleRows, compareModuleRows } from "./moduleListModel";
import { tanksFixture } from "@/protoOS/features/tanks/Tanks.fixtures";
import { asEntityKey } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

describe("module inventory", () => {
  test("updates PSU attention from retained telemetry when the clock recovers", () => {
    const { topology, telemetry } = tanksFixture(1, 1);
    const module = [...topology.modules.values()][0];
    const now = 200_000;
    telemetry.psus.get(module.psuIds[0])!.rawObservations = {
      electrical: [],
      temperatures: [],
      thermalState: [{ frameAt: now, observedAt: now, health: "degraded" }],
    };
    useMinerStore.getState().resetDeviceData();
    try {
      const store = () => useMinerStore.getState().telemetry;
      store().mergeTelemetry(telemetry);
      const hasErrors = () => buildModuleRows(topology, store())[0].hasErrors;
      store().refreshPsuReadings(now);
      expect(hasErrors()).toBe(true);
      store().refreshPsuReadings(NaN);
      expect(hasErrors()).toBe(false);
      store().refreshPsuReadings(now + 1_000);
      expect(hasErrors()).toBe(true);
      store().refreshPsuReadings(now + 120_000);
      expect(hasErrors()).toBe(false);
    } finally {
      useMinerStore.getState().resetDeviceData();
    }
  });

  test("retains known faults and issues when a board position is missing", () => {
    const { topology, telemetry } = tanksFixture(1, 1);
    const module = [...topology.modules.values()][0];
    const boardId = module.hashboardIds[0];
    topology.hashboards.get(boardId)!.location = undefined;
    telemetry.hashboards.get(boardId)!.health = "fault";
    telemetry.hashboards.get(boardId)!.reportedIssueCount = 4;

    expect(buildModuleRows(topology, telemetry)[0]).toMatchObject({
      status: "attention",
      hasErrors: true,
      issues: "present",
      issueCount: 4,
      issuesComplete: false,
    });
  });

  test("uses actual tank positions for names and sorts numerically across tanks", () => {
    const { topology, telemetry } = tanksFixture(2, 12);
    const rows = buildModuleRows(topology, telemetry).sort((a, b) => compareModuleRows(a, b, "name", "asc"));
    expect(rows.slice(0, 3).map((row) => row.name)).toEqual(["T1-P1", "T1-P2", "T1-P3"]);
    expect(rows[9].name).toBe("T1-P10");
    expect(rows[12].name).toBe("T2-P1");
    expect(new Set(rows.map((row) => row.id)).size).toBe(24);
    const module = [...topology.modules.values()][0];
    module.location = undefined;
    expect(buildModuleRows(topology, telemetry)[0].name).toBe("T1-P?");
  });

  test("keeps absent health and hashrate distinct from healthy and zero", () => {
    const { topology, telemetry } = tanksFixture(1, 2);
    const [first, second] = [...topology.modules.values()];
    telemetry.rollupsByEntity.set(asEntityKey(first.id), { hashrate: { latest: { value: 0, units: "TH/s" } } });
    telemetry.rollupsByEntity.delete(asEntityKey(second.id));
    first.hashboardIds.forEach((id) => {
      telemetry.hashboards.get(id)!.reportedIssueCount = 0;
    });
    second.hashboardIds.pop();
    const rows = buildModuleRows(topology, telemetry);
    expect(rows[0]).toMatchObject({ hashrate: 0, issueCount: 0, issues: "none", status: "healthy" });
    expect(rows[1]).toMatchObject({ hashrate: null, issueCount: null, issues: "unknown", status: "unknown" });
  });

  test("keeps known issues visible when another board has no health reading", () => {
    const { topology, telemetry } = tanksFixture(1, 1);
    const module = [...topology.modules.values()][0];
    telemetry.hashboards.get(module.hashboardIds[0])!.reportedIssueCount = 2;
    telemetry.hashboards.get(module.hashboardIds[0])!.health = "fault";
    telemetry.hashboards.delete(module.hashboardIds[1]);
    expect(buildModuleRows(topology, telemetry)[0]).toMatchObject({
      issues: "present",
      issueCount: 2,
      issuesComplete: false,
      status: "attention",
    });
  });
  test("reads module metrics without treating missing or invalid readings as zero", () => {
    const { topology, telemetry } = tanksFixture(1, 4);
    const modules = [...topology.modules.values()];
    telemetry.rollupsByEntity.set(asEntityKey(modules[0].id), {
      temperature: { latest: { value: 65, units: "°C" } },
      power: { latest: { value: 4500, units: "W" } },
      efficiency: { latest: { value: 15, units: "J/TH" } },
    });
    telemetry.rollupsByEntity.set(asEntityKey(modules[1].id), {
      temperature: { latest: { value: 0, units: "°C" } },
      power: { latest: { value: 0, units: "W" } },
      efficiency: { latest: { value: 0, units: "J/TH" } },
    });
    telemetry.rollupsByEntity.delete(asEntityKey(modules[2].id));
    telemetry.rollupsByEntity.set(asEntityKey(modules[3].id), {
      temperature: { latest: { value: NaN, units: "°C" } },
      power: { latest: { value: Infinity, units: "W" } },
      efficiency: { latest: { value: null, units: "J/TH" } },
    });
    const rows = buildModuleRows(topology, telemetry);
    expect(rows[0]).toMatchObject({ temperature: 65, power: 4500, efficiency: 15 });
    expect(rows[1]).toMatchObject({ temperature: 0, power: 0, efficiency: 0 });
    expect(rows[2]).toMatchObject({ temperature: null, power: null, efficiency: null });
    expect(rows[3]).toMatchObject({ temperature: null, power: null, efficiency: null });
    for (const field of ["temperature", "power", "efficiency"] as const) {
      expect([...rows].sort((a, b) => compareModuleRows(a, b, field, "asc")).map((row) => row.name)).toEqual([
        "T1-P2",
        "T1-P1",
        "T1-P3",
        "T1-P4",
      ]);
      expect([...rows].sort((a, b) => compareModuleRows(a, b, field, "desc")).map((row) => row.name)).toEqual([
        "T1-P1",
        "T1-P2",
        "T1-P3",
        "T1-P4",
      ]);
    }
  });
});
