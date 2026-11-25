import { expect, it } from "vitest";
import { inventoryAncestorsFromURL } from "./inventoryFilterUrl";
import { EntityKind } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

it("uses typed container and tank identities independently of building IDs", () => {
  const ancestors = inventoryAncestorsFromURL(
    new URLSearchParams("container=7&container=8&container=7&tank=7&building=7"),
  );
  expect(ancestors.map(({ kind, id }) => ({ kind, id }))).toEqual([
    { kind: EntityKind.CONTAINER, id: 7n },
    { kind: EntityKind.CONTAINER, id: 8n },
    { kind: EntityKind.TANK, id: 7n },
  ]);
});

it("keeps tank scope out of the equipment-groups request", () => {
  expect(inventoryAncestorsFromURL(new URLSearchParams("container=7&tank=8"), false)).toHaveLength(1);
});

it("rejects nonpositive or malformed CT1 identities", () => {
  expect(
    inventoryAncestorsFromURL(new URLSearchParams("container=0&container=-1&container=bad&container=null&tank=NaN")),
  ).toEqual([]);
});
