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
  rollups: [
    { phase: "done", count: 96 },
    { phase: "inProgress", count: 24 },
    { phase: "queued", count: 98 },
    { phase: "failed", count: 4 },
    { phase: "excluded", count: 18 },
  ],
  issueGroups: [
    { label: "update timed out", count: 3 },
    { label: "checksum mismatch", count: 1 },
  ],
};

/** Paused at the pilot-approval gate — the pilot wave finished, awaiting a
 * Continue / Retry decision. */
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
  rollups: [
    { phase: "done", count: 8 },
    { phase: "failed", count: 2 },
    { phase: "queued", count: 212 },
    { phase: "excluded", count: 18 },
  ],
  issueGroups: [{ label: "update timed out", count: 2 }],
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
  issueGroups: [
    { label: "update timed out", count: 8 },
    { label: "checksum mismatch", count: 3 },
    { label: "unreachable", count: 1 },
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
