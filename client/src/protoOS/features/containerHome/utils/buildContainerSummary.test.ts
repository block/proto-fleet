import { describe, expect, it } from "vitest";
import { buildIssueSummary, buildModuleGrid } from "./buildContainerSummary";
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

  it("uses reported tank and module positions without closing gaps", () => {
    const { hardware, telemetry } = toCanonicalContainer(
      {
        containerMap: {
          tanks: [1, 3, 6].map((id) => ({
            id,
            modules: [1, 2, 3, 9, 16].map((slot) => ({
              id: slot,
              slots: [{ hashboard: { serial: `HB-${id}-${slot}` } }],
            })),
          })),
        },
      },
      {},
      {},
      "ct1",
    );
    const grid = buildModuleGrid(hardware, telemetry, [], "/miners/ct1");
    expect(grid?.tanks.map((tank) => tank.slot)).toEqual([1, 3, 6]);
    expect(grid?.tanks[0]).toMatchObject({ columns: 8, rows: 2 });
    expect(grid?.tanks[0].modules.map((module) => ({ label: module.label, position: module.position }))).toEqual([
      { label: "01", position: { row: 2, column: 1 } },
      { label: "02", position: { row: 1, column: 1 } },
      { label: "03", position: { row: 2, column: 2 } },
      { label: "09", position: { row: 2, column: 5 } },
      { label: "16", position: { row: 1, column: 8 } },
    ]);
    expect(grid?.tanks[0].modules[0].boards).toHaveLength(3);
  });

  it("does not expose isolation as an overview state", () => {
    const { hardware, telemetry } = toCanonicalContainer(
      {
        containerMap: {
          tanks: [
            {
              id: 1,
              modules: [
                {
                  id: 1,
                  slots: [1, 2, 3].map((slot) => ({
                    hashboard: { serial: `HB-${slot}`, isolated: true },
                  })),
                },
              ],
            },
          ],
        },
      },
      {},
      {},
      "ct1",
    );
    const grid = buildModuleGrid(hardware, telemetry, [], "");
    expect(grid?.tanks[0].modules[0].state).toBe("unknown");
    expect(grid?.tanks[0].modules[0].boards).toEqual(["unknown", "unknown", "unknown"]);
    expect(grid?.counts).not.toHaveProperty("isolated");
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

it("keeps unmapped positions blank but omitted positions unknown", () => {
  const { hardware, telemetry } = toCanonicalContainer(
    {
      containerMap: {
        tanks: [
          {
            id: 1,
            modules: [
              {
                id: 1,
                slots: [
                  {
                    occupied: true,
                    hashboard: {
                      serial: "HB1",
                      location: { hashboardPosition: 1 },
                    },
                  },
                  { occupied: false, configuredLocation: { hashboardPosition: 2 } },
                ],
              },
              {
                id: 2,
                slots: [1, 2, 3].map((hashboardPosition) => ({
                  occupied: false,
                  configuredLocation: { hashboardPosition },
                })),
              },
              { id: 3, slots: [] },
            ],
          },
        ],
      },
    },
    {},
    {},
    "ct1",
  );
  const grid = buildModuleGrid(hardware, telemetry, [], "");
  expect(grid?.tanks[0].modules.map((module) => module.boards)).toEqual([
    ["unknown", "empty", "unknown"],
    ["empty", "empty", "empty"],
    ["unknown", "unknown", "unknown"],
  ]);
  expect(Object.values(grid!.counts).reduce((sum, count) => sum + count, 0)).toBe(2);
});

it.each([
  ["stopped", false, "sleeping", "attention"],
  ["unknown", true, "offline", "offline"],
  ["mining", false, "attention", "attention"],
] as const)("uses pill priority for %s with board errors", (miningStatus, disconnected, state, boardState) => {
  const { hardware, telemetry } = toCanonicalContainer(
    {
      containerMap: {
        tanks: [
          {
            id: 1,
            modules: [
              {
                id: 1,
                slots: [1, 2, 3].map((position) => ({
                  occupied: true,
                  hashboard: {
                    serial: `HB${position}`,
                    location: { hashboardPosition: position },
                  },
                })),
              },
            ],
          },
        ],
      },
    },
    {},
    {},
    "ct1",
  );
  [...telemetry.hashboards.values()].forEach((board, index) => {
    board.miningStatus = miningStatus;
    board.disconnected = disconnected;
    board.faultSeverity = index === 2 ? "critical" : null;
  });
  const cell = buildModuleGrid(hardware, telemetry, [], "")?.tanks[0].modules[0];
  expect(cell?.state).toBe(state);
  expect(cell?.boards[2]).toBe(boardState);
});

it("uses warning severity without treating a module PSU report as a board fault", () => {
  const { hardware, telemetry } = toCanonicalContainer(
    {
      containerMap: {
        tanks: [
          {
            id: 1,
            modules: [
              {
                id: 1,
                slots: [1, 2, 3].map((position) => ({
                  occupied: true,
                  hashboard: {
                    serial: `HB${position}`,
                    location: { hashboardPosition: position },
                  },
                })),
              },
            ],
          },
        ],
      },
    },
    {},
    {},
    "ct1",
  );
  [...telemetry.hashboards.values()].forEach((board, index) => {
    board.miningStatus = "mining";
    board.health = index === 0 ? "fault" : "ok";
    board.faultSeverity = index === 1 ? "warning" : null;
  });
  expect(buildModuleGrid(hardware, telemetry, [], "")?.tanks[0].modules[0]).toMatchObject({
    state: "attention",
    boards: ["healthy", "warning", "healthy"],
    attentionSeverity: "warning",
  });
});

it("does not mark a fully accounted-for partial module unknown", () => {
  const { hardware, telemetry } = toCanonicalContainer(
    {
      containerMap: {
        tanks: [
          {
            id: 1,
            modules: [
              {
                id: 1,
                slots: [
                  { hashboard: { serial: "HB1", location: { hashboardPosition: 1 } } },
                  { hashboard: { serial: "HB2", location: { hashboardPosition: 2 } } },
                  { occupied: false, configuredLocation: { hashboardPosition: 3 } },
                ],
              },
            ],
          },
        ],
      },
    },
    {},
    {},
    "ct1",
  );
  if (hardware.topology?.kind !== "container") throw new Error("container topology expected");
  const module = [...hardware.topology.modules.values()][0];
  const psu = "psu" as ComponentId;
  module.psuIds = [psu];
  telemetry.psus.set(psu, { componentId: psu, health: "ok", observedAt: { thermalState: 0 } });
  for (const board of telemetry.hashboards.values()) board.miningStatus = "mining";
  expect(buildModuleGrid(hardware, telemetry, [], "", 1_000)?.tanks[0].modules[0]).toMatchObject({
    state: "healthy",
    boards: ["healthy", "healthy", "empty"],
  });
});
