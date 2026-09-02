/**
 * Vocabulary for firmware release channels. These types are presentational
 * scaffolding for the rollout-framework Storybook prototype.
 */

import type { RolloutPlanConfig } from "./rolloutTypes";

/** Visual treatment for release-channel update state. */
export type ReleaseChannelUpdateTone = "attention" | "active" | "completed" | "none";

/** A hardware-model cohort within a release channel. */
export interface ReleaseChannelModelCohort {
  id: string;
  model: string;
  minerCount: number;
  /** The last installed version, omitted when there is no prior release. */
  previousVersion?: string;
  /** The currently installed or targeted version. */
  currentVersion: string;
  updateStatus: string;
  updateTone: ReleaseChannelUpdateTone;
}

/** A row in the Release channels table. */
export interface ReleaseChannel {
  id: string;
  name: string;
  /** Miners currently subscribed to this channel. */
  minerCount: number;
  /** Firmware files published to this channel. */
  releaseCount: number;
  /** Human-formatted last-updated timestamp, e.g. "11/7/25 9:01 PM". */
  lastUpdated: string;
  /** Summary of active model-level updates. */
  updateStatus: string;
  updateTone: ReleaseChannelUpdateTone;
  modelCohorts: ReleaseChannelModelCohort[];
}

/** A firmware file published to a channel. */
export interface ReleaseChannelFile {
  id: string;
  /** Hardware model the image targets, e.g. "Antminer S21". */
  model: string;
  /** Image file name, e.g. "miner-image-release-123". */
  file: string;
  /** Human-formatted upload timestamp. */
  uploaded: string;
}

/** The cohort a channel applies to, expressed as counts per scope level. */
export interface ReleaseChannelScope {
  sites: number;
  buildings: number;
  racks: number;
  groups: number;
  miners: number;
}

/**
 * The live coverage preview shown in the modal's right pane: a one-line summary
 * of what this channel deploys to, and the upcoming rollout timeline.
 */
export interface ReleaseChannelPreview {
  minerCount: number;
  modelCount: number;
  siteCount: number;
  buildingCount: number;
  rackCount: number;
  /** Human-formatted upcoming rollout timestamps, most recent first. */
  previousRollouts: string[];
}

/** Everything the Manage release channel modal edits or displays. */
export interface ReleaseChannelDraft {
  name: string;
  description: string;
  files: ReleaseChannelFile[];
  scope: ReleaseChannelScope;
  config: RolloutPlanConfig;
}
