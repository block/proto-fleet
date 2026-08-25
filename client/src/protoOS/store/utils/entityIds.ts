import {
  asComponentId,
  asHashboardId,
  type ComponentId,
  type HashboardId,
  type ModuleId,
  ROOT_MODULE_ID,
} from "../contract";

export const getHashboardId = (serial: string): HashboardId =>
  asHashboardId(serial.startsWith("serial:") ? serial : `serial:${serial}`);

export const getPsuId = (slot: number, moduleId: ModuleId = ROOT_MODULE_ID): ComponentId =>
  asComponentId(`${moduleId}:psu:${slot}`);

export const getFanId = (slot: number, moduleId: ModuleId = ROOT_MODULE_ID): ComponentId =>
  asComponentId(`${moduleId}:fan:${slot}`);
