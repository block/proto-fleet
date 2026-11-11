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

/** What every owner answered to a cancel. An owner that could not be reached rejects the cancel instead. */
export interface CT1FirmwareCancelResult {
  /** An owner accepted the cancel but its boards are still stopping. */
  pending: boolean;
  /** An owner's job had already ended on its own, so its per-board result still has to be shown. */
  finished: boolean;
  /** Owners that do not know the task. They stay unresolved: the task may still reach them. */
  absent: string[];
}

/** Start was definitively refused: the controller created no job, so there is nothing to recover. */
export class CT1FirmwareStartRefusedError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "CT1FirmwareStartRefusedError";
  }
}

export interface CT1Firmware {
  prepare(params?: AdapterRequestOptions): Promise<CT1FirmwareBoard[]>;
  /** Rejects with `CT1FirmwareStartRefusedError` only when Start was definitively refused after `onStarting`. */
  install(
    image: CT1FirmwareImage,
    attempt: CT1FirmwareAttempt,
    onProgress: (percent: number) => void,
    params: AdapterRequestOptions | undefined,
    onStarting: () => void,
  ): Promise<void>;
  progress(attempt: CT1FirmwareAttempt, params?: AdapterRequestOptions): Promise<CT1FirmwareProgress>;
  cancel(attempt: CT1FirmwareAttempt, params?: AdapterRequestOptions): Promise<CT1FirmwareCancelResult>;
}
