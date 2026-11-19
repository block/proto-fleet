import { describe, expect, it } from "vitest";
import type { buildContainerLive } from "./buildContainerHome";
import { buildIssueSummary, buildModuleGrid, buildSummaryKpis } from "./buildContainerSummary";
import { toCanonicalContainer } from "@/protoOS/api/adapters/mdkV2/normalize";
import type {
  ComponentId,
  ContainerId,
  HashboardId,
  HashboardTelemetry,
  MinerError,
  ModuleId,
  PsuTelemetry,
} from "@/protoOS/store/contract";

const moduleOwner = { kind: "module" as const, id: "module:1" as ModuleId };
const boardError = (board: string, code: string, severity: MinerError["severity"]): MinerError => ({
  id: `${board}:${code}`,
  code,
  message: code,
  owner: moduleOwner,
  component: { kind: "hashboard", id: board as HashboardId, owner: moduleOwner },
  severity,
});

describe("container summary", () => {
  it("groups repeated issues by name and counts the affected components once", () => {
    const summary = buildIssueSummary([
      boardError("hb-1", "HASHBOARD_ERROR_CAUSE_WIRING_STALE", "warning"),
      boardError("hb-1", "HASHBOARD_ERROR_CAUSE_WIRING_STALE", "warning"),
      boardError("hb-2", "HASHBOARD_ERROR_CAUSE_WIRING_STALE", "warning"),
      boardError("hb-3", "HASHBOARD_ERROR_CAUSE_OVER_TEMPERATURE", "info"),
      {
        id: "alarm",
        code: "ac_1",
        title: "Air conditioner #1 fault",
        message: "Critical: Air conditioner #1 fault",
        owner: { kind: "container", id: "container:ct1" as ContainerId },
        severity: "critical",
      },
    ]);
    expect(summary).toEqual({
      critical: 1,
      warning: 1,
      groups: [
        expect.objectContaining({ label: "Air conditioner #1 fault", severity: "critical", affected: "1 container" }),
        expect.objectContaining({ label: "Wiring stale", severity: "warning", affected: "2 boards" }),
      ],
    });
  });

  it("compares hashrate with ideal only when both are reported", () => {
    const live = {
      hashrate: 97,
      idealHashrate: 100,
      power: 2_000,
      efficiency: 20.6,
      moduleEfficiency: null,
      rollup: { boardCount: 4, onlineBoardCount: 3 },
    } as ReturnType<typeof buildContainerLive>;
    const kpis = buildSummaryKpis(live);
    expect(kpis[0]).toMatchObject({ text: "97.0% of 100.0 TH/s ideal", chartPercentage: 97 });
    expect(kpis[3]).toMatchObject({ value: "3 / 4", text: "1 inactive" });
    expect(buildSummaryKpis({ ...live, idealHashrate: null })[0].text).toBeUndefined();
  });

  it("keeps a sleeping tank as sleeping when another tank is mining", () => {
    const tank = (id: number) => ({ id, modules: [{ id: 1, slots: [{ hashboard: { serial: `HB-${id}` } }] }] });
    const { hardware, telemetry } = toCanonicalContainer(
      { containerMap: { tanks: [tank(1), tank(2)] } },
      {},
      {},
      "ct1",
    );
    if (hardware.topology?.kind !== "container") throw new Error("container topology expected");
    // A module needs three hashboards and a PSU before its state is known.
    [...hardware.topology.modules.values()].forEach((module, index) => {
      const psu = `psu-${index}` as ComponentId;
      module.hashboardIds = [1, 2, 3].map((slot) => `hb-${index}-${slot}` as HashboardId);
      module.psuIds = [psu];
      telemetry.psus.set(psu, { health: "ok", observedAt: { thermalState: 0 } } as PsuTelemetry);
      for (const id of module.hashboardIds)
        telemetry.hashboards.set(id, {
          health: "ok",
          miningStatus: index === 0 ? "stopped" : "mining",
        } as HashboardTelemetry);
    });
    const grid = buildModuleGrid(hardware, telemetry, [], "/miners/ct1", 1_000);
    expect(grid?.tanks.map((row) => row.modules.map((module) => module.state))).toEqual([["sleeping"], ["healthy"]]);
    expect(grid?.counts).toMatchObject({ sleeping: 1, healthy: 1, attention: 0 });
    expect(grid?.tanks[0].modules[0].to).toMatch(/^\/miners\/ct1\/tanks\/.+\/modules\/.+/);
    // A PSU sample that is too old no longer supports a healthy module.
    const stale = buildModuleGrid(hardware, telemetry, [], "/miners/ct1", 121_000);
    expect(stale?.tanks[1].modules[0].state).toBe("unknown");
  });
});
