import type { AdapterRequestOptions } from "./types";

/** The builds a hashboard release ships. Image selection and its error text both follow this list. */
export const hashboardFirmwareBuilds = ["prod", "dev", "mfgtest"] as const;
export type HashboardFirmwareBuild = (typeof hashboardFirmwareBuilds)[number];

export interface HashboardFirmwareImage {
  file: File;
  build: HashboardFirmwareBuild;
  version: string;
}

export interface HashboardFirmwareBoard {
  mac: string;
  owner: string;
  label: string;
}

/** Persist before Start: an uncertain result must never create another task. */
export interface HashboardFirmwareAttempt {
  taskId: string;
  /**
   * Set once the operator has tried to cancel, and saved with the record so a reload shows the same state.
   * "requested": every owner accepted the cancel and its boards are still stopping. "unconfirmed": an owner was
   * unreachable or does not know the task, so only the operator can release the record.
   */
  cancel?: "requested" | "unconfirmed";
  boards: HashboardFirmwareBoard[];
  version: string;
  build: string;
}

/** How an update ended, from the owners' job statuses. When owners end differently, the worst is reported. */
export type HashboardFirmwareOutcome = "completed" | "completedWithErrors" | "canceled" | "interrupted" | "failed";

export interface HashboardFirmwareProgress {
  boards: Array<HashboardFirmwareBoard & { status: string; percent?: number; message?: string }>;
  /**
   * Set once every owner reports an ended job; absent while any owner is unresolved. A board that an ended job does
   * not list is named in `errors` and ranks the update as at least "completedWithErrors".
   */
  outcome?: HashboardFirmwareOutcome;
  errors: string[];
}

/** What every owner answered to a cancel. An owner that could not be reached rejects the cancel instead. */
export interface HashboardFirmwareCancelResult {
  /** An owner accepted the cancel but its boards are still stopping. */
  pending: boolean;
  /** An owner's job had already ended on its own, so its per-board result still has to be shown. */
  finished: boolean;
  /** Owners that do not know the task. They stay unresolved: the task may still reach them. */
  absent: string[];
}

/** Start was definitively refused: the controller created no job, so there is nothing to recover. */
export class HashboardFirmwareStartRefusedError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "HashboardFirmwareStartRefusedError";
  }
}

/** A cancel that some owners could not answer. `failures` holds one line per owner, worded as status errors are. */
export class HashboardFirmwareCancelError extends Error {
  constructor(readonly failures: string[]) {
    super(failures.join("; "));
    this.name = "HashboardFirmwareCancelError";
  }
}

export interface HashboardFirmware {
  /** Reads a chosen file as an image for this miner's hashboards, or rejects naming the files it accepts. */
  readImage(file: File): Promise<HashboardFirmwareImage>;
  prepare(params?: AdapterRequestOptions): Promise<HashboardFirmwareBoard[]>;
  /** Rejects with `HashboardFirmwareStartRefusedError` only when Start was definitively refused after `onStarting`. */
  install(
    image: HashboardFirmwareImage,
    attempt: HashboardFirmwareAttempt,
    onProgress: (percent: number) => void,
    params: AdapterRequestOptions | undefined,
    onStarting: () => void,
  ): Promise<void>;
  progress(attempt: HashboardFirmwareAttempt, params?: AdapterRequestOptions): Promise<HashboardFirmwareProgress>;
  /** Rejects with `HashboardFirmwareCancelError` when an owner could not be reached. */
  cancel(attempt: HashboardFirmwareAttempt, params?: AdapterRequestOptions): Promise<HashboardFirmwareCancelResult>;
}
