import { create } from "@bufbuild/protobuf";
import { parseCT1FilterValuesFromURL } from "./filterUrlParams";
import { EntityKind, EntityRefSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

export const inventoryAncestorsFromURL = (params: URLSearchParams, includeTanks = true) => {
  const containers = parseCT1FilterValuesFromURL(params, "container").map((id) =>
    create(EntityRefSchema, { kind: EntityKind.CONTAINER, id: BigInt(id) }),
  );
  const tanks = includeTanks
    ? parseCT1FilterValuesFromURL(params, "tank").map((id) =>
        create(EntityRefSchema, { kind: EntityKind.TANK, id: BigInt(id) }),
      )
    : [];
  return [...containers, ...tanks];
};
