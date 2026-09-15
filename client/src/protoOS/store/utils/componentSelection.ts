import { type ComponentRef, type HardwareState, type ModuleId, ROOT_MODULE_ID } from "../contract";
import type { ErrorSource } from "../types";

export type ComponentAddress = ComponentRef | { source: ErrorSource; slot?: number; moduleId?: ModuleId };

/** Resolve a slot once, within its module. All subsequent reads use the returned ID. */
export function resolveComponentAddress(
  address: ComponentAddress | undefined,
  hardware: HardwareState,
): ComponentRef | undefined {
  if (!address) return undefined;
  if ("kind" in address) return address;
  const { source, slot } = address;
  if (source !== "RIG" && slot === undefined) return undefined;
  const moduleId = address.moduleId ?? ROOT_MODULE_ID;
  if (source === "HASHBOARD") {
    const node = [...(hardware.topology?.hashboards.values() ?? [])].find(
      (board) => board.moduleId === moduleId && board.location?.slot === slot,
    );
    return node ? { kind: "hashboard", id: node.id, owner: { kind: "module", id: node.moduleId } } : undefined;
  }
  const components = source === "PSU" ? hardware.psus : source === "FAN" ? hardware.fans : hardware.controlBoards;
  return [...components.values()].find(
    (item) =>
      item.ref.owner.kind === "module" &&
      item.ref.owner.id === moduleId &&
      (source === "RIG" || ("slot" in item && item.slot === slot)),
  )?.ref;
}

export function componentName(ref: ComponentRef, hardware: HardwareState): string {
  const node = ref.kind === "hashboard" ? hardware.topology?.hashboards.get(ref.id) : undefined;
  const module = ref.owner.kind === "module" ? hardware.topology?.modules.get(ref.owner.id) : undefined;
  const slot =
    node?.location?.slot ??
    (ref.kind === "psu"
      ? hardware.psus.get(ref.id)?.slot
      : ref.kind === "fan"
        ? hardware.fans.get(ref.id)?.slot
        : undefined);
  const labels: Record<ComponentRef["kind"], string> = {
    hashboard: module?.profile === "ct1" ? "Board" : "Hashboard",
    psu: "PSU",
    fan: "Fan",
    controlBoard: "Control board",
    asic: "ASIC",
    plant: "Plant",
  };
  const kind = labels[ref.kind];
  const name = `${kind}${slot === undefined ? "" : ` ${slot}`}`;
  if (!module?.tankId) return name;
  const tank = hardware.topology?.kind === "container" ? hardware.topology.tanks.get(module.tankId) : undefined;
  return [
    tank?.label ?? module.location?.tankLabel ?? module.tankId,
    `Module ${module.location?.moduleSlot ?? module.id}`,
    name,
  ].join(" · ");
}
