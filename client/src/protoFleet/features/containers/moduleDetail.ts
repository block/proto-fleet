import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { tankPath } from "./buildTankOverview";
import type { ContainerOverviewData } from "./hooks/useContainerOverview";
import { EntityKind, type InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

export function modulePath(item: InventoryItem): string | undefined {
  const tank = item.ancestors.find((parent) => parent.kind === EntityKind.TANK);
  if (item.ref?.kind !== EntityKind.MODULE || !item.deviceIdentifier || !tank || item.state === "retired")
    return undefined;
  return `${tankPath(item.deviceIdentifier, String(tank.id))}/modules/${item.ref.id}`;
}

export function resolveModule(data: ContainerOverviewData, tankId: string, moduleId: string) {
  const inContainer = (item: InventoryItem) =>
    item.ancestors.some((parent) => parent.kind === EntityKind.CONTAINER && parent.id === data.container.id);
  const tank = data.tanks.find(
    (item) => item.ref?.kind === EntityKind.TANK && String(item.ref.id) === tankId && inContainer(item),
  );
  if (!tank) return undefined;
  const modules = data.modules.filter(
    (item) =>
      item.ref?.kind === EntityKind.MODULE &&
      item.state !== "retired" &&
      inContainer(item) &&
      item.ancestors.some((parent) => parent.kind === EntityKind.TANK && parent.id === tank.ref!.id),
  );
  const module = modules.find((item) => String(item.ref!.id) === moduleId);
  return module ? { tank, module, modules: [...modules].sort((a, b) => a.position - b.position) } : undefined;
}

export function isCurrentObservation(at: Timestamp | undefined, now = Date.now()) {
  if (!at) return false;
  const age = now - (Number(at.seconds) * 1000 + at.nanos / 1e6);
  return age >= -5000 && age < 120_000;
}
