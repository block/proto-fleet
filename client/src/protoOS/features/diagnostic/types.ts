import type { HashboardInfo } from "@/protoOS/api/generatedApi";
import { HashboardHardwareData } from "@/protoOS/store";
import type { ComponentType } from "@/shared/components/StatusModal";

// Re-export component types from shared
export type { ComponentType, ErrorData } from "@/shared/components/StatusModal";

// Component metadata types
export interface FanMetadata {
  serialNumber?: string;
  manufacturer?: string;
  model?: string;
  firmwareVersion?: string;
}

export interface PsuMetadata {
  firmwareAppVersion?: string;
  firmwareBootloaderVersion?: string;
  hardwareRevision?: string;
  manufacturer?: string;
  model?: string;
  serialNumber?: string;
  vendor?: string;
}

export interface ControlBoardMetadata {
  serialNumber?: string;
  boardId?: string;
  machineName?: string;
  firmwareName?: string;
  firmwareVersion?: string;
  firmwareVariant?: string;
  gitHash?: string;
  hardware?: string;
  modelName?: string;
}

// Component data types
export interface FanData {
  id: number;
  position: number;
  name: string;
  rpm: number;
  pwm: number;
  hasWarning?: boolean;
  meta: FanMetadata;
}

export interface PsuData {
  id: number;
  name: string;
  position: number;
  inputVoltage: number;
  outputVoltage: number;
  inputPower: number;
  outputPower: number;
  avgTemp: number | undefined | null;
  maxTemp: number | undefined | null;
  hasWarning?: boolean;
  meta: PsuMetadata;
}

export interface ControlBoardData {
  name: string;
  latency: number;
  cpuCapacity: number;
  hasWarning?: boolean;
  meta: ControlBoardMetadata;
}

export type HashboardData = HashboardInfo & {
  position: number;
  hasWarning: boolean;
  loading: boolean;
};

// Union types for slot states
export type EmptySlot = {
  isEmpty: true;
  position: number;
  type: ComponentType;
};

export type HashboardSlot = HashboardHardwareData | EmptySlot;
export type FanSlot = FanData | EmptySlot;
export type PsuSlot = PsuData | EmptySlot;

// Type guards
export const isEmptySlot = (slot: any): slot is EmptySlot => slot.isEmpty === true;
