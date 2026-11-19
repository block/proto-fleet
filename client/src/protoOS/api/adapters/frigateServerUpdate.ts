import type { FrigateUpdateMessage } from "./frigateUpdateMessages";
import type { AdapterRequestOptions } from "./types";
import { newUUID } from "@/protoOS/utils/uuid";

export const newFrigateJobId = newUUID;

/**
 * A Frigate release as published: the Debian package plus its signed release statement. ProtoOS only checks that the
 * three files agree with each other and with the controllers. The controllers verify the signature against their own
 * trusted keys when the update starts, and that is the trust decision.
 */
export interface FrigatePackage {
  deb: File;
  /** The statement's exact bytes, as signed. */
  statement: Uint8Array;
  signature: Uint8Array;
  version: string;
  architecture: string;
  repository: string;
  sha256: string;
  sizeBytes: number;
  minInstalledVersion?: string;
  issuedAt?: string;
}

/** What one controller reports about its own update support. */
export interface FrigateController {
  instanceId: string;
  runningVersion: string;
  architecture: string;
  spoolFreeBytes: number;
  trustedKeyIds: string[];
}

export interface FrigateUpdatePlan {
  /** The controller this page is connected to comes first. */
  controllers: FrigateController[];
  /** Plain-language reasons the update can't start now. Empty when it can. */
  blockers: string[];
  /** Set when a controller reports a rollout in progress. Follow that rollout instead of starting another. */
  activeJobId?: string;
}

/**
 * Where a local update stands. "staging" and "staged" can be discarded. "starting" means Start was sent and its answer
 * is not known yet; "started" means the controllers have the rollout.
 */
export type FrigateUpdateStep = "staging" | "staged" | "starting" | "started";

/**
 * Saved before staging and updated before Start, so a reload can discard an abandoned stage and an uncertain Start is
 * checked by status rather than sent again.
 */
export interface FrigateUpdateRecord {
  jobId: string;
  /** Start's idempotency key. Sending Start again with it returns the existing rollout instead of a second one. */
  idempotencyKey: string;
  targetVersion: string;
  packageName: string;
  instanceIds: string[];
  /** Each instance's manifest fingerprint from staging. Start fails unless every one still matches. */
  fingerprints: Record<string, string>;
  /**
   * What identifies the package to a controller, saved before copying. A controller can accept the start of a copy
   * and its answer be lost, leaving no fingerprint here; Discard asks again with this to learn it.
   */
  release?: FrigateUpdateRelease;
  step: FrigateUpdateStep;
  /** Why the last Start attempt did not start the update. */
  notStarted?: FrigateUpdateMessage;
  /**
   * While "starting": until this time (ms since the epoch) Start may still be in flight, so a status answer without
   * the rollout doesn't yet mean it wasn't created. Absent once the controller refused Start.
   */
  startSettlesAt?: number;
  /**
   * A retry or cancel that status hasn't shown taking effect yet. The same key replays its recorded outcome. `since`
   * is the rollout's `updatedAt` when it was sent: a rollout that changed after that has used the key up.
   */
  pendingAction?: FrigateUpdateAction;
}

/** The statement and signature are base64, so the record can be saved as JSON. */
export interface FrigateUpdateRelease {
  sha256: string;
  sizeBytes: number;
  statement: string;
  signature: string;
}

export const encodeBytes = (bytes: Uint8Array) => {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
};
export const decodeBytes = (text: string) => Uint8Array.from(atob(text), (char) => char.charCodeAt(0));

export const releaseOf = (pkg: FrigatePackage): FrigateUpdateRelease => ({
  sha256: pkg.sha256,
  sizeBytes: pkg.sizeBytes,
  statement: encodeBytes(pkg.statement),
  signature: encodeBytes(pkg.signature),
});

export interface FrigateUpdateAction {
  kind: "retry" | "cancel";
  key: string;
  since?: number;
}

export type FrigateRolloutPhase =
  | "verifying"
  | "stopping"
  | "installing"
  | "migrating"
  | "starting"
  | "succeeded"
  | "failed"
  | "cancelled"
  | "verified"
  | "abandoned"
  | "unknown";

export interface FrigateUpdateProblem {
  code: string;
  message: string;
}

export interface FrigateRolloutInstance {
  instanceId: string;
  /** The update helper's last reported phase, worded for display. */
  phase: string;
  runningVersion?: string;
  lastContact?: Date;
  contactStale: boolean;
  problem?: FrigateUpdateProblem;
}

export interface FrigateRollout {
  jobId: string;
  targetVersion: string;
  fromVersion: string;
  phase: FrigateRolloutPhase;
  /** No more changes will happen to this rollout unless it is retried. */
  ended: boolean;
  actor: string;
  releaseKeyId: string;
  retryable: boolean;
  cancellable: boolean;
  /** When Frigate last changed this rollout, in milliseconds. */
  updatedAt?: number;
  problem?: FrigateUpdateProblem;
  nextStep?: string;
  instances: FrigateRolloutInstance[];
}

export interface FrigateUpdateStatus {
  /** Absent when the controllers have no rollout with that job id (or none at all). */
  rollout?: FrigateRollout;
  /** Frigate is stopped for the update and its maintenance service answered. */
  servedByMaintenance: boolean;
}

/**
 * The controller answered with a failure. `code` is Frigate's reason, or empty when it gave none. The reason is for
 * wording only: unless this is a `FrigateUpdateRefusedError`, a write may still have been applied.
 */
export class FrigateUpdateError extends Error {
  constructor(
    readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = "FrigateUpdateError";
  }
}

/** The controller refused before changing anything. */
export class FrigateUpdateRefusedError extends FrigateUpdateError {
  constructor(code: string, message: string) {
    super(code, message);
    this.name = "FrigateUpdateRefusedError";
  }
}

export interface FrigateServerUpdate {
  /** Reads the chosen release files, or rejects naming what is missing or inconsistent. */
  readPackage(files: File[], params?: AdapterRequestOptions): Promise<FrigatePackage>;
  /** Reads both controllers' support. Package checks are added when `pkg` is given. */
  plan(pkg: FrigatePackage | undefined, params?: AdapterRequestOptions): Promise<FrigateUpdatePlan>;
  /**
   * Copies the package to every controller in the record and returns each one's manifest fingerprint. `onProgress`
   * also reports the fingerprints learned so far, so an interrupted copy can still be discarded.
   */
  stage(
    pkg: FrigatePackage,
    record: FrigateUpdateRecord,
    onProgress: (percent: number, fingerprints: Record<string, string>) => void,
    params?: AdapterRequestOptions,
  ): Promise<Record<string, string>>;
  /**
   * Removes a staged package that was never started. Rejects when a controller may still hold it: with code
   * `UPDATE_ACTIVE_JOB` when the update has started after all, otherwise with that controller's failure.
   */
  discard(record: FrigateUpdateRecord, params?: AdapterRequestOptions): Promise<void>;
  /** Never retried. Rejects with `FrigateUpdateRefusedError` only when the controller refused before any write. */
  start(record: FrigateUpdateRecord, params?: AdapterRequestOptions): Promise<FrigateRollout>;
  /** With no job id, the active rollout or else the most recent one. */
  status(jobId: string | undefined, params?: AdapterRequestOptions): Promise<FrigateUpdateStatus>;
  retry(jobId: string, idempotencyKey: string, params?: AdapterRequestOptions): Promise<FrigateRollout>;
  cancel(jobId: string, idempotencyKey: string, params?: AdapterRequestOptions): Promise<FrigateRollout>;
}
