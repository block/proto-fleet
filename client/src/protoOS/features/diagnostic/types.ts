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
