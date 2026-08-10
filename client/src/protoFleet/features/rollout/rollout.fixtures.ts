import type {
  RolloutEvent,
  RolloutMinerRow,
  RolloutPlanConfig,
  RolloutState,
  RolloutTargetPhase,
} from "./rolloutTypes";

/** Base batched firmware plan config, as configured in the modal. */
export const batchedFirmwareConfig: RolloutPlanConfig = {
  processType: "firmware",
  strategy: "batched",
  order: "leastEfficientFirst",
  maxConcurrentOffline: 50,
  batchSize: 20,
  batchIntervalSec: 60,
  scheduleType: "startNow",
};

export const allAtOnceFirmwareConfig: RolloutPlanConfig = {
  processType: "firmware",
  strategy: "allAtOnce",
  order: "leastEfficientFirst",
  maxConcurrentOffline: 50,
  scheduleType: "startNow",
};

export const pilotFirmwareConfig: RolloutPlanConfig = {
  processType: "firmware",
  strategy: "pilotThenContinue",
  order: "leastEfficientFirst",
  maxConcurrentOffline: 50,
  pilotSize: 10,
  batchSize: 25,
  batchIntervalSec: 90,
  scheduleType: "scheduleForLater",
  scheduledStartAt: "2026-08-14T20:00:00.000Z",
};

/** Base batched reboot plan config for the generic config modal. */
export const batchedRebootConfig: RolloutPlanConfig = {
  processType: "reboot",
  strategy: "batched",
  order: "leastEfficientFirst",
  maxConcurrentOffline: 20,
  batchSize: 10,
  batchIntervalSec: 45,
  scheduleType: "startNow",
};

/** Default reboot plan config for the generic config modal. */
export const allAtOnceRebootConfig: RolloutPlanConfig = {
  processType: "reboot",
  strategy: "allAtOnce",
  order: "leastEfficientFirst",
  maxConcurrentOffline: 40,
  scheduleType: "startNow",
};

/** Base batched curtailment plan config, for the generic config modal. */
export const batchedCurtailmentConfig: RolloutPlanConfig = {
  processType: "curtailment",
  strategy: "batched",
  order: "leastEfficientFirst",
  maxConcurrentOffline: 60,
  batchSize: 60,
  batchIntervalSec: 30,
  scheduleType: "startNow",
};

type RolloutMinerIdentity = Omit<RolloutMinerRow, "phase">;

const rolloutMinerIdentities: RolloutMinerIdentity[] = [
  {
    id: "miner-b03-01",
    name: "b03-s21-01",
    type: "Antminer S21",
    ipAddress: "10.18.32.41",
    hashrate: { value: "199.4 TH/s", delta: "-0.3%" },
    power: { value: "3.46 kW", delta: "-0.2%" },
    efficiency: { value: "17.4 J/TH", delta: "+0.1%" },
    temperature: { value: "62.4 C", delta: "+0.4 C" },
  },
  {
    id: "miner-b03-02",
    name: "b03-s21-02",
    type: "Antminer S21",
    ipAddress: "10.18.32.42",
    hashrate: { value: "201.1 TH/s", delta: "+0.2%" },
    power: { value: "3.50 kW", delta: "+0.1%" },
    efficiency: { value: "17.4 J/TH", delta: "+0.1%" },
    temperature: { value: "63.0 C", delta: "+0.6 C" },
  },
  {
    id: "miner-b03-03",
    name: "b03-s21-03",
    type: "Antminer S21",
    ipAddress: "10.18.32.43",
    hashrate: { value: "197.8 TH/s", delta: "-0.8%" },
    power: { value: "3.45 kW", delta: "-0.3%" },
    efficiency: { value: "17.5 J/TH", delta: "+0.2%" },
    temperature: { value: "64.1 C", delta: "+1.0 C" },
  },
  {
    id: "miner-b04-01",
    name: "b04-s21-01",
    type: "Antminer S21 Pro",
    ipAddress: "10.18.33.11",
    hashrate: { value: "228.6 TH/s", delta: "+0.4%" },
    power: { value: "3.70 kW", delta: "+0.1%" },
    efficiency: { value: "16.2 J/TH", delta: "+0.1%" },
    temperature: { value: "61.8 C", delta: "+0.2 C" },
  },
  {
    id: "miner-b04-02",
    name: "b04-s21-02",
    type: "Antminer S21 Pro",
    ipAddress: "10.18.33.12",
    hashrate: { value: "226.9 TH/s", delta: "-0.5%" },
    power: { value: "3.69 kW", delta: "-0.1%" },
    efficiency: { value: "16.3 J/TH", delta: "+0.2%" },
    temperature: { value: "62.6 C", delta: "+0.7 C" },
  },
  {
    id: "miner-b04-03",
    name: "b04-s21-03",
    type: "Antminer S21 Pro",
    ipAddress: "10.18.33.13",
    hashrate: { value: "0 TH/s", delta: "Offline" },
    power: { value: "0.14 kW" },
    efficiency: { value: "Offline" },
    temperature: { value: "42.0 C" },
  },
  {
    id: "miner-b05-01",
    name: "b05-m60-01",
    type: "Whatsminer M60",
    ipAddress: "10.18.34.21",
    hashrate: { value: "188.2 TH/s", delta: "-1.1%" },
    power: { value: "3.51 kW", delta: "-0.2%" },
    efficiency: { value: "18.7 J/TH", delta: "+0.4%" },
    temperature: { value: "65.5 C", delta: "+1.5 C" },
  },
  {
    id: "miner-b05-02",
    name: "b05-m60-02",
    type: "Whatsminer M60",
    ipAddress: "10.18.34.22",
    hashrate: { value: "190.7 TH/s", delta: "+0.1%" },
    power: { value: "3.52 kW", delta: "+0.1%" },
    efficiency: { value: "18.5 J/TH", delta: "+0.3%" },
    temperature: { value: "64.7 C", delta: "+0.9 C" },
  },
  {
    id: "miner-b05-03",
    name: "b05-m60-03",
    type: "Whatsminer M60",
    ipAddress: "10.18.34.23",
    hashrate: { value: "186.4 TH/s", delta: "-1.8%" },
    power: { value: "3.49 kW", delta: "-0.3%" },
    efficiency: { value: "18.7 J/TH", delta: "+0.5%" },
    temperature: { value: "66.2 C", delta: "+1.8 C" },
  },
  {
    id: "miner-b06-01",
    name: "b06-s19-01",
    type: "Antminer S19 XP",
    ipAddress: "10.18.35.31",
    hashrate: { value: "138.2 TH/s" },
    power: { value: "3.02 kW" },
    efficiency: { value: "21.9 J/TH" },
    temperature: { value: "60.9 C" },
  },
  {
    id: "miner-b06-02",
    name: "b06-s19-02",
    type: "Antminer S19 XP",
    ipAddress: "10.18.35.32",
    hashrate: { value: "139.0 TH/s" },
    power: { value: "3.04 kW" },
    efficiency: { value: "21.9 J/TH" },
    temperature: { value: "61.3 C" },
  },
  {
    id: "miner-b06-03",
    name: "b06-s19-03",
    type: "Antminer S19 XP",
    ipAddress: "10.18.35.33",
    hashrate: { value: "0 TH/s" },
    power: { value: "0 kW" },
    efficiency: { value: "Pinned" },
    temperature: { value: "Idle" },
  },
];

const phaseSamplesByState: Record<RolloutState, RolloutTargetPhase[]> = {
  scheduled: ["queued", "queued", "queued", "queued", "excluded"],
  inProgress: ["done", "done", "inProgress", "inProgress", "retrying", "queued", "queued", "failed", "excluded"],
  pausedAtPilotGate: ["done", "done", "done", "failed", "queued", "queued", "excluded"],
  paused: ["done", "done", "done", "queued", "queued", "queued", "excluded"],
  completed: ["done", "done", "done", "done", "done", "done", "excluded"],
  completedWithFailures: ["done", "done", "done", "failed", "failed", "done", "excluded"],
};

export function rolloutMinerRowsForEvent(event: RolloutEvent): RolloutMinerRow[] {
  const phaseSamples = phaseSamplesByState[event.state];

  return rolloutMinerIdentities.map((miner, index) => ({
    ...miner,
    id: `${event.processType}-${miner.id}`,
    phase: phaseSamples[index % phaseSamples.length],
  }));
}

/** A firmware rollout mid-flight, the in-progress detail card. */
export const inProgressFirmwareEvent: RolloutEvent = {
  processType: "firmware",
  state: "inProgress",
  title: "Firmware update to 5.1.0",
  scopeLabel: "Building B",
  strategy: "batched",
  order: "leastEfficientFirst",
  totalTargets: 240,
  excludedTargets: 18,
  batchSize: 20,
  batchIntervalSec: 60,
  currentBatch: 5,
  totalBatches: 12,
  startedAt: new Date(Date.now() - 372_000).toISOString(),
  estimatedSecondsRemaining: 420,
  // Cohort holding its pre-rollout performance, the mid-flight deltas are all
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

/** Paused at the pilot-approval gate, the pilot wave finished, awaiting a
 * Continue / Cancel remaining decision. */
export const pilotGateFirmwareEvent: RolloutEvent = {
  processType: "firmware",
  state: "pausedAtPilotGate",
  title: "Firmware update to 5.1.0",
  scopeLabel: "Building B",
  strategy: "pilotThenContinue",
  order: "leastEfficientFirst",
  totalTargets: 240,
  excludedTargets: 18,
  batchSize: 25,
  batchIntervalSec: 90,
  currentBatch: 1,
  totalBatches: 10,
  startedAt: new Date(Date.now() - 180_000).toISOString(),
  // Baseline captured when the rollout started. The readout shows the numbers;
  // the Continue/Cancel remaining call stays with the operator.
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

/** Finished with some failures, the retained activity record. */
export const completedWithFailuresFirmwareEvent: RolloutEvent = {
  processType: "firmware",
  state: "completedWithFailures",
  title: "Firmware update to 5.1.0",
  scopeLabel: "Building B",
  strategy: "batched",
  order: "leastEfficientFirst",
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

/** Scheduled but not yet started, everything queued, waiting on the start
 * time. */
export const scheduledFirmwareEvent: RolloutEvent = {
  processType: "firmware",
  state: "scheduled",
  title: "Firmware update to 5.1.0",
  scopeLabel: "Building B",
  strategy: "batched",
  order: "leastEfficientFirst",
  totalTargets: 240,
  excludedTargets: 18,
  batchSize: 20,
  batchIntervalSec: 60,
  totalBatches: 12,
  scheduledStartAt: "2026-08-14T20:00:00.000Z",
  rollups: [
    { phase: "queued", count: 222 },
    { phase: "excluded", count: 18 },
  ],
};

/** Paused mid-flight, dispatch halted, holding at the current batch. */
export const pausedFirmwareEvent: RolloutEvent = {
  processType: "firmware",
  state: "paused",
  title: "Firmware update to 5.1.0",
  scopeLabel: "Building B",
  strategy: "batched",
  order: "leastEfficientFirst",
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

/** Finished cleanly, the retained activity record with no failures. */
export const completedFirmwareEvent: RolloutEvent = {
  processType: "firmware",
  state: "completed",
  title: "Firmware update to 5.1.0",
  scopeLabel: "Building B",
  strategy: "batched",
  order: "leastEfficientFirst",
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

/** A reboot rollout for stacked-banner stories. */
export const inProgressRebootEvent: RolloutEvent = {
  processType: "reboot",
  state: "inProgress",
  title: "Reboot",
  scopeLabel: "Rack A3",
  strategy: "batched",
  order: "leastEfficientFirst",
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

/** A reboot paused mid-flight, holding at the current batch. */
export const pausedRebootEvent: RolloutEvent = {
  processType: "reboot",
  state: "paused",
  title: "Reboot",
  scopeLabel: "Rack A3",
  strategy: "batched",
  order: "leastEfficientFirst",
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

/** A reboot finished cleanly, every target back online. */
export const completedRebootEvent: RolloutEvent = {
  processType: "reboot",
  state: "completed",
  title: "Reboot",
  scopeLabel: "Rack A3",
  strategy: "batched",
  order: "leastEfficientFirst",
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
  order: "leastEfficientFirst",
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
  order: "leastEfficientFirst",
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
