import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { isCurrentObservation, modulePath, resolveModule } from "./moduleDetail";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const container = create(ContainerSchema, { id: 7n, deviceIdentifier: "ct1" });
const tank = create(InventoryItemSchema, {
  ref: { kind: EntityKind.TANK, id: 81n },
  ancestors: [{ kind: EntityKind.CONTAINER, id: 7n }],
});
const module = create(InventoryItemSchema, {
  ref: { kind: EntityKind.MODULE, id: 91n },
  deviceIdentifier: "ct1",
  position: 1,
  ancestors: [
    { kind: EntityKind.CONTAINER, id: 7n },
    { kind: EntityKind.TANK, id: 81n },
  ],
});

describe("module detail identity", () => {
  it("builds the same destination for every module entry point", () => {
    expect(modulePath(module)).toBe("/containers/ct1/tanks/81/modules/91");
    expect(modulePath({ ...module, deviceIdentifier: "" })).toBeUndefined();
    expect(modulePath({ ...module, ancestors: [] })).toBeUndefined();
  });
  it("resolves a stable module ID and both ancestors", () => {
    const data = { container, tanks: [tank], modules: [module] };
    expect(resolveModule(data, "81", "91")?.module).toBe(module);
    expect(resolveModule(data, "82", "91")).toBeUndefined();
    expect(resolveModule({ ...data, container: { ...container, id: 8n } }, "81", "91")).toBeUndefined();
    expect(resolveModule({ ...data, modules: [{ ...module, state: "retired" }] }, "81", "91")).toBeUndefined();
    expect(
      resolveModule({ ...data, modules: [{ ...module, ref: { ...module.ref!, id: 92n } }] }, "81", "91"),
    ).toBeUndefined();
  });
  it("does not treat missing, old, or future timestamps as current", () => {
    const now = Date.now();
    expect(isCurrentObservation(timestampFromMs(now), now)).toBe(true);
    expect(isCurrentObservation(undefined, now)).toBe(false);
    expect(isCurrentObservation(timestampFromMs(now - 120_000), now)).toBe(false);
    expect(isCurrentObservation(timestampFromMs(now + 6_000), now)).toBe(false);
  });
});

it.each(["tank", "container"])("rejects module %s mismatches after the requested tank resolves", (mismatch) => {
  const otherTank = create(InventoryItemSchema, { ancestors: tank.ancestors, ref: { kind: EntityKind.TANK, id: 82n } });
  const misplaced = create(InventoryItemSchema, {
    ref: module.ref,
    ancestors: [
      { kind: EntityKind.CONTAINER, id: mismatch === "container" ? 8n : 7n },
      { kind: EntityKind.TANK, id: mismatch === "tank" ? 82n : 81n },
    ],
  });
  expect(resolveModule({ container, tanks: [tank, otherTank], modules: [misplaced] }, "81", "91")).toBeUndefined();
});
