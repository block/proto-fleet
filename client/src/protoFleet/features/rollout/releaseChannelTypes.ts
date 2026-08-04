/**
 * Vocabulary for the firmware **release channels** surface — the persistent
 * tracks (Dev / Staging / Production, …) that determine which firmware versions
 * a cohort of miners receives over time, and the rollout settings that govern
 * how a new release rolls out along a channel.
 *
 * Presentational scaffolding for the rollout-framework Storybook prototype: the
 * stories drive these with fixtures. A release channel wraps the same
 * `RolloutPlanConfig` the rest of the framework uses, so its "Rollout" section
 * reuses `RolloutControls` verbatim.
 */

import type { RolloutPlanConfig } from "./rolloutTypes";

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
}

/** A firmware file published to a channel, shown in the modal's Firmware table. */
export interface ReleaseChannelFile {
  id: string;
  /** Miner type the image targets, e.g. "Rig" or "Antminer S19". */
  type: string;
  /** Image file name, e.g. "miner-image-release-123". */
  file: string;
  /** Human-formatted upload timestamp. */
  uploaded: string;
}

/**
 * The cohort a channel applies to, expressed as counts per scope level. Mirrors
 * curtailment's "Apply to" scope: each level maps to a `TargetSelectButton`
 * whose label folds the count in (0 → "Select").
 */
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
