import {
  asComponentId,
  asHashboardId,
  type ComponentId,
  type HashboardId,
  type ModuleId,
  ROOT_MODULE_ID,
} from "../contract";

const MAC_ADDRESS = /^(?:[0-9a-f]{2}:){5}[0-9a-f]{2}$/i;

export const getHashboardId = (identity: string): HashboardId => {
  if (identity.startsWith("serial:") || identity.startsWith("mac:")) return asHashboardId(identity);
  return asHashboardId(MAC_ADDRESS.test(identity) ? `mac:${identity}` : `serial:${identity}`);
};

export const getPsuId = (slot: number, moduleId: ModuleId = ROOT_MODULE_ID): ComponentId =>
  asComponentId(`${moduleId}:psu:${slot}`);

export const getFanId = (slot: number, moduleId: ModuleId = ROOT_MODULE_ID): ComponentId =>
  asComponentId(`${moduleId}:fan:${slot}`);

// Inverse of the slot-based id builders above: pull the trailing slot number back
// out of a `${moduleId}:<kind>:<slot>` component id (0 when absent).
export const getComponentSlot = (componentId: string): number => Number(componentId.match(/:(\d+)$/)?.[1] ?? 0);
