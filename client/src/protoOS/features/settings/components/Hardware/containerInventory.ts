import type { HardwareState, ModuleId, SystemInfoData } from "@/protoOS/store/contract";

export interface InventoryItem {
  id: string;
  label: string;
  model?: string;
  serialNumber?: string;
  firmware?: string;
}

export interface InventoryGroup {
  name: string;
  items?: InventoryItem[];
}

/** Only reported inventory is counted; plant telemetry is not a physical parts list. */
export const containerInventory = (hardware?: HardwareState, systemInfo?: SystemInfoData): InventoryGroup[] => {
  const topology = hardware?.topology;
  if (!hardware || topology?.kind !== "container")
    return [
      "Tanks",
      "Modules",
      "PSUs",
      "Hashboards",
      "Controllers",
      "CDU",
      "Coolant pumps",
      "Dry cooler fans",
      "PDUs",
      "A/C",
      "Lights",
      "Control cabinet",
      "Alarm",
    ].map((name) => ({ name }));

  const moduleLabel = (id: ModuleId) => {
    const module = topology.modules.get(id);
    const tank = module?.tankId ? topology.tanks.get(module.tankId) : undefined;
    return tank?.slot !== undefined && module?.location?.moduleSlot !== undefined
      ? `T${tank.slot}-M${String(module.location.moduleSlot).padStart(2, "0")}`
      : id.replace(/^.*:module:/, "");
  };
  const reported = (items: InventoryItem[]) => (items.length ? items : undefined);
  const controllers: InventoryItem[] = [...hardware.controlBoards.values()].map((board, index) => ({
    id: board.ref.id,
    label: board.machineName || `Control server ${index + 1}`,
    model: board.boardId,
    serialNumber: board.serialNumber,
    firmware: board.firmware?.version,
  }));
  // System info describes the responding controller, not every server in the container.
  if (!controllers.length && systemInfo?.controlBoard) {
    controllers.push({
      id: systemInfo.identity.deviceId,
      label: systemInfo.controlBoard.machineName || "Control server",
      model: systemInfo.controlBoard.model,
      serialNumber: systemInfo.controlBoard.serialNumber,
      firmware: systemInfo.operatingSystem?.version,
    });
  }

  return [
    {
      name: "Tanks",
      items: [...topology.tanks.values()].map((tank) => ({ id: tank.id, label: tank.label || tank.id })),
    },
    {
      name: "Modules",
      items: [...topology.modules.values()].map((module) => ({ id: module.id, label: moduleLabel(module.id) })),
    },
    {
      name: "PSUs",
      items: reported(
        [...hardware.psus.values()].map((psu) => ({
          id: psu.ref.id,
          label: psu.ref.owner.kind === "module" ? moduleLabel(psu.ref.owner.id) : `PSU ${psu.slot ?? ""}`,
          model: psu.model,
          serialNumber: psu.serialNumber,
          firmware: psu.firmware?.application?.version,
        })),
      ),
    },
    {
      name: "Hashboards",
      items: [...topology.hashboards.values()].map((board) => {
        const details = hardware.hashboards.get(board.id);
        return {
          id: board.id,
          label: `${moduleLabel(board.moduleId)}-${board.location?.slot ?? "?"}`,
          model: details?.boardModel,
          serialNumber: details?.serialNumber || (board.identity.kind === "serial" ? board.identity.value : undefined),
          firmware: details?.firmware?.version,
        };
      }),
    },
    { name: "Controllers", items: reported(controllers) },
    { name: "CDU" },
    { name: "Coolant pumps" },
    {
      name: "Dry cooler fans",
      items: reported(
        [...hardware.fans.values()].map((fan) => ({
          id: fan.ref.id,
          label: fan.name || `Fan ${fan.slot ?? ""}`,
        })),
      ),
    },
    {
      name: "PDUs",
      items: reported(
        [...hardware.plant.values()]
          .filter((part) => part.kind === "distribution")
          .map((part) => ({
            id: part.ref.id,
            label: part.name,
          })),
      ),
    },
    { name: "A/C" },
    { name: "Lights" },
    { name: "Control cabinet" },
    { name: "Alarm" },
  ].map((group) => ({
    ...group,
    items: group.items?.sort((a, b) => a.label.localeCompare(b.label, undefined, { numeric: true })),
  }));
};
