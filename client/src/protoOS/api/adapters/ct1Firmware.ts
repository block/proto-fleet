import type { AdapterRequestOptions } from "./types";

export interface CT1FirmwareImage {
  file: File;
  build: "prod" | "dev" | "mfgtest";
  version: string;
}

export interface CT1FirmwareBoard {
  mac: string;
  owner: string;
  label: string;
}

/** Persist before Start: an uncertain result must never create another task. */
export interface CT1FirmwareAttempt {
  taskId: string;
  cancelRequested?: boolean;
  boards: CT1FirmwareBoard[];
  version: string;
  build: string;
}

export interface CT1FirmwareProgress {
  boards: Array<CT1FirmwareBoard & { status: string; percent?: number; message?: string }>;
  complete: boolean;
  errors: string[];
}

export interface CT1Firmware {
  prepare(params?: AdapterRequestOptions): Promise<CT1FirmwareBoard[]>;
  install(
    image: CT1FirmwareImage,
    attempt: CT1FirmwareAttempt,
    onProgress: (percent: number) => void,
    params: AdapterRequestOptions | undefined,
    onStarting: () => void,
  ): Promise<void>;
  progress(attempt: CT1FirmwareAttempt, params?: AdapterRequestOptions): Promise<CT1FirmwareProgress>;
  cancel(
    attempt: CT1FirmwareAttempt,
    params?: AdapterRequestOptions,
  ): Promise<{ pending: boolean; boards: CT1FirmwareBoard[] }>;
}
