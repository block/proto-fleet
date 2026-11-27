import { expect, test } from "vitest";
import { getScopeHealth } from "./scopeHealth";
import { moduleFixture } from "@/protoOS/api/adapters/mdkV2/moduleFixtures";
import { tanksFixture } from "@/protoOS/features/tanks/Tanks.fixtures";
import { asHashboardId, type ContainerTopology, type MinerError } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

const storeModuleFixture = () => {
  const fixture = moduleFixture("noPsu");
  const store = useMinerStore.getState();
  store.resetDeviceData();
  store.hardware.replaceHardware(fixture.hardware);
  store.telemetry.mergeTelemetry(fixture.telemetry);
  return useMinerStore.getState();
};

test("scopes faults by module ownership, counts components once, and includes PSU-only faults", () => {
  const { hardware, telemetry } = moduleFixture();
  const modules = [...hardware.topology!.modules.values()];
  const psu = hardware.psus.get(modules[0].psuIds[0])!;
  const board = hardware.hashboards.get(modules[1].hashboardIds[0])!;
  const errors: MinerError[] = [psu.ref, psu.ref, board.ref].map((component, index) => ({
    id: String(index),
    code: "fault",
    message: `Fault ${index}`,
    severity: "critical",
    component,
    owner: component.owner,
  }));
  const result = getScopeHealth({ kind: "module", id: modules[0].id }, hardware, telemetry, errors)!;
  expect(result.errors.map((error) => error.message)).toEqual(["Fault 0", "Fault 1"]);
  expect(result.affectedComponents).toBe(1);
  expect(result.moduleStatuses.get(modules[0].id)).toBe("attention");
  expect(result.title).toBe("Needs attention");
});

test("tank scope excludes another tank even when module and board slots match", () => {
  const { hardware, telemetry, topology } = tanksFixture(2, 2);
  const tanks = [...topology.tanks.values()];
  const refs = tanks.map(
    (tank) => hardware.hashboards.get(topology.modules.get(tank.moduleIds[0])!.hashboardIds[0])!.ref,
  );
  const errors: MinerError[] = refs.map((component, index) => ({
    id: String(index),
    code: "fault",
    message: `Tank ${index + 1} fault`,
    severity: "critical",
    component,
    owner: component.owner,
  }));
  expect(
    getScopeHealth({ kind: "tank", id: tanks[1].id }, hardware, telemetry, errors)!.errors.map((e) => e.message),
  ).toEqual(["Tank 2 fault"]);
});

test("reports health-only faults and unknown readings without manufacturing active errors", () => {
  const { hardware, telemetry } = moduleFixture();
  const topology = hardware.topology as ContainerTopology;
  const module = [...topology.modules.values()][0];
  const board = telemetry.hashboards.get(module.hashboardIds[0])!;
  board.health = "fault";
  let result = getScopeHealth({ kind: "module", id: module.id }, hardware, telemetry, [])!;
  expect(result.errors).toEqual([]);
  expect(result.observations.some((item) => item.message === "Fault reported; details unavailable")).toBe(true);
  expect(result.affectedComponents).toBe(1);
  board.health = "ok";
  telemetry.psus.delete(module.psuIds[0]);
  result = getScopeHealth({ kind: "module", id: module.id }, hardware, telemetry, [])!;
  expect(result.title).toBe("Status unknown");
  expect(result.observations.some((item) => item.component.id === module.psuIds[0])).toBe(true);
});

test("marks module, tank, and container health unknown when a module has no PSU", () => {
  const { hardware, telemetry } = storeModuleFixture();
  const topology = hardware.topology as ContainerTopology;
  const tank = [...topology.tanks.values()][0];
  const module = topology.modules.get(tank.moduleIds[0])!;

  expect(module.psuIds).toEqual([]);
  for (const scope of [
    { kind: "module", id: module.id },
    { kind: "tank", id: tank.id },
    { kind: "container" },
  ] as const) {
    expect(getScopeHealth(scope, hardware, telemetry, [])!.title).toBe("Status unknown");
  }
});

test.each([
  ["fault", "attention"],
  ["offline", "offline"],
] as const)("does not let a missing PSU suppress %s hashboard health", (health, expectedStatus) => {
  const initial = storeModuleFixture();
  const topology = initial.hardware.topology as ContainerTopology;
  const tank = [...topology.tanks.values()][0];
  const module = topology.modules.get(tank.moduleIds[0])!;
  useMinerStore.setState((state) => {
    state.telemetry.hashboards.get(module.hashboardIds[0])!.health = health;
  });
  const { hardware, telemetry } = useMinerStore.getState();

  for (const scope of [
    { kind: "module", id: module.id },
    { kind: "tank", id: tank.id },
    { kind: "container" },
  ] as const) {
    const result = getScopeHealth(scope, hardware, telemetry, [])!;
    expect(result.title).toBe("Needs attention");
    expect(result.moduleStatuses.get(module.id)).toBe(expectedStatus);
  }
});

test.each(["fault", "degraded"] as const)("isolating a board does not hide its %s health", (health) => {
  const { hardware, telemetry } = moduleFixture();
  const module = [...hardware.topology!.modules.values()][0];
  Object.assign(telemetry.hashboards.get(module.hashboardIds[0])!, { health, isolated: true });
  const result = getScopeHealth({ kind: "module", id: module.id }, hardware, telemetry, [])!;
  expect(result.affectedComponents).toBe(1);
  expect(result.observations[0].component.id).toBe(module.hashboardIds[0]);
  expect(result.title).toBe("Needs attention");
});

test("explains a partially sleeping module with a component drill-down", () => {
  const { hardware, telemetry } = moduleFixture();
  const module = [...hardware.topology!.modules.values()][0];
  telemetry.hashboards.get(module.hashboardIds[0])!.miningStatus = "stopped";
  const result = getScopeHealth({ kind: "module", id: module.id }, hardware, telemetry, [])!;
  expect(result.affectedComponents).toBe(1);
  expect(result.observations).toContainEqual({
    component: hardware.hashboards.get(module.hashboardIds[0])!.ref,
    message: "Sleeping",
  });
});

test.each([
  { miningStatus: "stopped", isolated: false, title: "Sleeping" },
  { miningStatus: "curtailed", isolated: false, title: "Sleeping" },
  { miningStatus: "mining", isolated: true, title: "Isolated" },
] as const)("explains wholly $title modules in a partially operational scope", ({ miningStatus, isolated, title }) => {
  const { hardware, telemetry, topology } = tanksFixture(2, 2);
  const [tank, otherTank] = [...topology.tanks.values()];
  const module = topology.modules.get(tank.moduleIds[1])!;
  for (const id of module.hashboardIds) Object.assign(telemetry.hashboards.get(id)!, { miningStatus, isolated });

  for (const scope of [{ kind: "tank", id: tank.id }, { kind: "container" }] as const) {
    const result = getScopeHealth(scope, hardware, telemetry, [])!;
    expect(result.title).toBe("Needs attention");
    expect(result.affectedComponents).toBe(3);
    expect(result.affectedModules).toBe(1);
    expect(result.errors).toEqual([]);
    expect(result.observations).toEqual(
      module.hashboardIds.map((id) => ({
        component: hardware.hashboards.get(id)!.ref,
        message: title,
      })),
    );
  }
  expect(getScopeHealth({ kind: "module", id: module.id }, hardware, telemetry, [])!.title).toBe(title);
  expect(getScopeHealth({ kind: "tank", id: otherTank.id }, hardware, telemetry, [])!.title).toBe(
    "All components operational",
  );

  for (const reading of telemetry.hashboards.values()) Object.assign(reading, { miningStatus, isolated });
  for (const scope of [{ kind: "tank", id: tank.id }, { kind: "container" }] as const) {
    const result = getScopeHealth(scope, hardware, telemetry, [])!;
    expect(result.title).toBe(title);
    expect(result.affectedComponents).toBe(0);
    expect(result.observations).toEqual([]);
  }
});

test("does not call a mixture of sleeping and isolated modules operational", () => {
  const { hardware, telemetry } = moduleFixture();
  const modules = [...hardware.topology!.modules.values()];
  for (const reading of telemetry.hashboards.values()) reading.miningStatus = "stopped";
  for (const id of modules[0].hashboardIds) telemetry.hashboards.get(id)!.isolated = true;
  const result = getScopeHealth({ kind: "container" }, hardware, telemetry, [])!;
  expect(result.title).toBe("Needs attention");
  expect(result.affectedComponents).toBe(telemetry.hashboards.size);
  expect(result.affectedModules).toBe(modules.length);
});

test.each(["unknown", "offline"] as const)("keeps partial-module observations when another module is %s", (health) => {
  const { hardware, telemetry } = moduleFixture();
  const [partial, unavailable] = [...hardware.topology!.modules.values()];
  telemetry.hashboards.get(partial.hashboardIds[0])!.miningStatus = "stopped";
  for (const id of unavailable.hashboardIds)
    Object.assign(telemetry.hashboards.get(id)!, { health, miningStatus: "unknown" });
  const result = getScopeHealth({ kind: "container" }, hardware, telemetry, [])!;
  expect(result.title).toBe("Needs attention");
  expect(result.observations).toContainEqual({
    component: hardware.hashboards.get(partial.hashboardIds[0])!.ref,
    message: "Sleeping",
  });
});

const withUnlocatedBoard = () => {
  const fixture = tanksFixture(1, 1);
  const id = asHashboardId("unlocated");
  fixture.topology.hashboards.set(id, { id, identity: { kind: "serial", value: "unlocated" }, asicIds: [] });
  fixture.telemetry.hashboards.set(id, {
    hashboardId: id,
    health: "ok",
    miningStatus: "mining",
    healthObservedAt: Date.now(),
  });
  return { ...fixture, id };
};

test.each(["missing", "expired", "future"] as const)(
  "marks %s unlocated health unknown beside healthy modules",
  (age) => {
    const { hardware, telemetry, topology, id } = withUnlocatedBoard();
    const now = Date.now();
    telemetry.hashboards.get(id)!.healthObservedAt =
      age === "missing" ? undefined : now + (age === "expired" ? -120_000 : 60_000);
    const result = getScopeHealth({ kind: "container" }, hardware, telemetry, [], now)!;
    expect(result.title).toBe("Status unknown");
    expect(result.observations).toContainEqual({
      component: {
        kind: "hashboard",
        id,
        owner: { kind: "container", id: topology.containerId },
      },
      message: "Readings unavailable",
    });
    expect(result.affectedModules).toBe(0);
  },
);

test.each([true, false])(
  "includes unlocated boards in mixed mining states (physical sleeping: %s)",
  (physicalSleeping) => {
    const { hardware, telemetry, topology, id } = withUnlocatedBoard();
    for (const [boardId, reading] of telemetry.hashboards) {
      reading.miningStatus = (boardId === id ? !physicalSleeping : physicalSleeping) ? "stopped" : "mining";
    }
    const result = getScopeHealth({ kind: "container" }, hardware, telemetry, [])!;
    expect(result.title).toBe("Needs attention");
    expect(result.affectedModules).toBe(physicalSleeping ? 1 : 0);
    expect(result.affectedComponents).toBe(physicalSleeping ? 3 : 1);
    const module = [...topology.modules.values()][0];
    expect(getScopeHealth({ kind: "module", id: module.id }, hardware, telemetry, [])!.title).toBe(
      physicalSleeping ? "Sleeping" : "All components operational",
    );
  },
);

test("derives status from unlocated boards when no physical modules exist", () => {
  const { hardware, telemetry, topology, id } = withUnlocatedBoard();
  topology.modules.clear();
  topology.tanks.clear();
  for (const boardId of topology.hashboards.keys()) if (boardId !== id) topology.hashboards.delete(boardId);
  const result = getScopeHealth({ kind: "container" }, hardware, telemetry, [])!;
  expect(result.title).toBe("All components operational");
  expect(result.affectedModules).toBe(0);
});
