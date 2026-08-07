import type { RolloutEvent, RolloutPlanConfig } from "./rolloutTypes";

/** Base batched firmware plan config, as configured in the modal. */
export const batchedFirmwareConfig: RolloutPlanConfig = {
  processType: "firmware",
  strategy: "batched",
  order: "lowestPerformersFirst",
  maxConcurrentOffline: 50,
  batchSize: 20,
  batchIntervalSec: 60,
  scheduleType: "startNow",
};

export const allAtOnceFirmwareConfig: RolloutPlanConfig = {
  processType: "firmware",
  strategy: "allAtOnce",
  order: "lowestPerformersFirst",
  maxConcurrentOffline: 50,
  scheduleType: "startNow",
};

export const pilotFirmwareConfig: RolloutPlanConfig = {
  processType: "firmware",
  strategy: "pilotThenContinue",
  order: "lowestPerformersFirst",
  maxConcurrentOffline: 50,
  pilotSize: 10,
  batchSize: 25,
  batchIntervalSec: 90,
  scheduleType: "scheduleForLater",
  scheduledStartAt: "2026-08-04T20:00:00.000Z",
};

/** Base batched reboot plan config, for the generic (process-agnostic) config
 * modal — a process with no bespoke product modal of its own today. */
export const batchedRebootConfig: RolloutPlanConfig = {
  processType: "reboot",
  strategy: "batched",
  order: "lowestPerformersFirst",
  maxConcurrentOffline: 20,
  batchSize: 10,
  batchIntervalSec: 45,
  scheduleType: "startNow",
};

/**
 * The **default** reboot plan config — "all at once", immediate. Per the
 * rollout design review, a plain reboot should need zero pacing setup and match
 * today's behavior (everything reboots together); batching/scheduling stay
 * available as advanced options a user can opt into. This is what the reboot
 * bulk action seeds its config modal with.
 */
export const allAtOnceRebootConfig: RolloutPlanConfig = {
  processType: "reboot",
  strategy: "allAtOnce",
  order: "lowestPerformersFirst",
  maxConcurrentOffline: 40,
  scheduleType: "startNow",
};

/** Base batched curtailment plan config, for the generic config modal. */
export const batchedCurtailmentConfig: RolloutPlanConfig = {
  processType: "curtailment",
  strategy: "batched",
  order: "lowestPerformersFirst",
  maxConcurrentOffline: 60,
  batchSize: 60,
  batchIntervalSec: 30,
  scheduleType: "startNow",
};

/** A firmware rollout mid-flight — the in-progress detail card. */
export const inProgressFirmwareEvent: RolloutEvent = {
  processType: "firmware",
  state: "inProgress",
  title: "Firmware update to 5.1.0",
  scopeLabel: "Building B",
  strategy: "batched",
  order: "lowestPerformersFirst",
  totalTargets: 240,
  excludedTargets: 18,
  batchSize: 20,
  batchIntervalSec: 60,
  currentBatch: 5,
  totalBatches: 12,
  startedAt: new Date(Date.now() - 372_000).toISOString(),
  estimatedSecondsRemaining: 420,
  // Cohort holding its pre-rollout performance — the mid-flight deltas are all
  // small: hashrate/power read a slight red "−", efficiency/temp a slight green
  // "+". The healthy mid-flight case.
  performance: {
    metrics: [
      { label: "Hashrate", unit: "hashrate", baseline: 1600, current: 1596 },
      { label: "Power", unit: "power", baseline: 28.0, current: 27.9 },
      { label: "Efficiency", unit: "efficiency", baseline: 17.5, current: 17.55 },
      // Temperature stored in Celsius; rendered in the operator's °C/°F preference.
      { label: "Avg temp", unit: "temperature", baseline: 62.0, current: 62.4 },
    ],
  },
  rollups: [
    { phase: "done", count: 96 },
    { phase: "inProgress", count: 24 },
    { phase: "retrying", count: 6 },
    { phase: "queued", count: 92 },
    { phase: "failed", count: 4 },
    { phase: "excluded", count: 18 },
  ],
};

/** Paused at the pilot-approval gate — the pilot wave finished, awaiting a
 * Continue / Cancel decision. */
export const pilotGateFirmwareEvent: RolloutEvent = {
  processType: "firmware",
  state: "pausedAtPilotGate",
  title: "Firmware update to 5.1.0",
  scopeLabel: "Building B",
  strategy: "pilotThenContinue",
  order: "lowestPerformersFirst",
  totalTargets: 240,
  excludedTargets: 18,
  batchSize: 25,
  batchIntervalSec: 90,
  currentBatch: 1,
  totalBatches: 10,
  startedAt: new Date(Date.now() - 180_000).toISOString(),
  // Baseline captured when the rollout started, against the pilot cohort's
  // current telemetry. Hashrate/power dipped slightly (red "−"); efficiency has
  // crept up ~1.3% and avg temp ~1.9% (green "+"). Option A only surfaces the
  // numbers — the Continue/Cancel call stays with the operator.
  performance: {
    metrics: [
      { label: "Hashrate", unit: "hashrate", baseline: 1600, current: 1593 },
      { label: "Power", unit: "power", baseline: 28.0, current: 27.8 },
      { label: "Efficiency", unit: "efficiency", baseline: 17.5, current: 17.72 },
      // Temperature stored in Celsius; rendered in the operator's °C/°F preference.
      { label: "Avg temp", unit: "temperature", baseline: 63.0, current: 64.2 },
    ],
  },
  rollups: [
    { phase: "done", count: 8 },
    { phase: "failed", count: 2 },
    { phase: "queued", count: 212 },
    { phase: "excluded", count: 18 },
  ],
};

/** Finished with some failures — the retained activity record. */
export const completedWithFailuresFirmwareEvent: RolloutEvent = {
  processType: "firmware",
  state: "completedWithFailures",
  title: "Firmware update to 5.1.0",
  scopeLabel: "Building B",
  strategy: "batched",
  order: "lowestPerformersFirst",
  totalTargets: 240,
  excludedTargets: 18,
  batchSize: 20,
  batchIntervalSec: 60,
  currentBatch: 12,
  totalBatches: 12,
  startedAt: new Date(Date.now() - 900_000).toISOString(),
  estimatedSecondsRemaining: 0,
  rollups: [
    { phase: "done", count: 210 },
    { phase: "failed", count: 12 },
    { phase: "excluded", count: 18 },
  ],
};

/** Scheduled but not yet started — everything queued, waiting on the start
 * time. */
export const scheduledFirmwareEvent: RolloutEvent = {
  processType: "firmware",
  state: "scheduled",
  title: "Firmware update to 5.1.0",
  scopeLabel: "Building B",
  strategy: "batched",
  order: "lowestPerformersFirst",
  totalTargets: 240,
  excludedTargets: 18,
  batchSize: 20,
  batchIntervalSec: 60,
  totalBatches: 12,
  scheduledStartAt: "2026-08-04T20:00:00.000Z",
  rollups: [
    { phase: "queued", count: 222 },
    { phase: "excluded", count: 18 },
  ],
};

/** Paused mid-flight — dispatch halted, holding at the current batch. */
export const pausedFirmwareEvent: RolloutEvent = {
  processType: "firmware",
  state: "paused",
  title: "Firmware update to 5.1.0",
  scopeLabel: "Building B",
  strategy: "batched",
  order: "lowestPerformersFirst",
  totalTargets: 240,
  excludedTargets: 18,
  batchSize: 20,
  batchIntervalSec: 60,
  currentBatch: 6,
  totalBatches: 12,
  startedAt: new Date(Date.now() - 480_000).toISOString(),
  rollups: [
    { phase: "done", count: 110 },
    { phase: "queued", count: 112 },
    { phase: "excluded", count: 18 },
  ],
};

/** Finished cleanly — the retained activity record with no failures. */
export const completedFirmwareEvent: RolloutEvent = {
  processType: "firmware",
  state: "completed",
  title: "Firmware update to 5.1.0",
  scopeLabel: "Building B",
  strategy: "batched",
  order: "lowestPerformersFirst",
  totalTargets: 240,
  excludedTargets: 18,
  batchSize: 20,
  batchIntervalSec: 60,
  currentBatch: 12,
  totalBatches: 12,
  startedAt: new Date(Date.now() - 900_000).toISOString(),
  estimatedSecondsRemaining: 0,
  rollups: [
    { phase: "done", count: 222 },
    { phase: "excluded", count: 18 },
  ],
};

/** A reboot rollout — for the stacked-banner + process-agnostic stories. */
export const inProgressRebootEvent: RolloutEvent = {
  processType: "reboot",
  state: "inProgress",
  title: "Reboot",
  scopeLabel: "Rack A3",
  strategy: "batched",
  order: "lowestPerformersFirst",
  totalTargets: 40,
  excludedTargets: 0,
  batchSize: 10,
  batchIntervalSec: 45,
  currentBatch: 3,
  totalBatches: 4,
  startedAt: new Date(Date.now() - 120_000).toISOString(),
  estimatedSecondsRemaining: 120,
  rollups: [
    { phase: "done", count: 28 },
    { phase: "inProgress", count: 6 },
    { phase: "queued", count: 6 },
  ],
};

/** A reboot paused mid-flight — holding at the current batch. */
export const pausedRebootEvent: RolloutEvent = {
  processType: "reboot",
  state: "paused",
  title: "Reboot",
  scopeLabel: "Rack A3",
  strategy: "batched",
  order: "lowestPerformersFirst",
  totalTargets: 40,
  excludedTargets: 0,
  batchSize: 10,
  batchIntervalSec: 45,
  currentBatch: 2,
  totalBatches: 4,
  startedAt: new Date(Date.now() - 90_000).toISOString(),
  rollups: [
    { phase: "done", count: 18 },
    { phase: "queued", count: 22 },
  ],
};

/** A reboot finished cleanly — every target back online. */
export const completedRebootEvent: RolloutEvent = {
  processType: "reboot",
  state: "completed",
  title: "Reboot",
  scopeLabel: "Rack A3",
  strategy: "batched",
  order: "lowestPerformersFirst",
  totalTargets: 40,
  excludedTargets: 0,
  batchSize: 10,
  batchIntervalSec: 45,
  currentBatch: 4,
  totalBatches: 4,
  startedAt: new Date(Date.now() - 240_000).toISOString(),
  estimatedSecondsRemaining: 0,
  rollups: [{ phase: "done", count: 40 }],
};

/** A reboot finished with a few targets that never came back. */
export const completedWithFailuresRebootEvent: RolloutEvent = {
  processType: "reboot",
  state: "completedWithFailures",
  title: "Reboot",
  scopeLabel: "Rack A3",
  strategy: "batched",
  order: "lowestPerformersFirst",
  totalTargets: 40,
  excludedTargets: 0,
  batchSize: 10,
  batchIntervalSec: 45,
  currentBatch: 4,
  totalBatches: 4,
  startedAt: new Date(Date.now() - 240_000).toISOString(),
  estimatedSecondsRemaining: 0,
  rollups: [
    { phase: "done", count: 37 },
    { phase: "failed", count: 3 },
  ],
};

/** A curtailment rollout, shown as a peer process in the stacked banners. */
export const inProgressCurtailmentEvent: RolloutEvent = {
  processType: "curtailment",
  state: "inProgress",
  title: "Curtailment",
  scopeLabel: "Whole site",
  strategy: "batched",
  order: "lowestPerformersFirst",
  totalTargets: 240,
  excludedTargets: 0,
  batchSize: 60,
  batchIntervalSec: 30,
  currentBatch: 2,
  totalBatches: 4,
  startedAt: new Date(Date.now() - 90_000).toISOString(),
  estimatedSecondsRemaining: 90,
  rollups: [
    { phase: "done", count: 118 },
    { phase: "inProgress", count: 40 },
    { phase: "queued", count: 82 },
  ],
};
