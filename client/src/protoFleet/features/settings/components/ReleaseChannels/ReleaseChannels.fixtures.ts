import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";

import {
  MetricComparisonSchema,
  type PreviewReleaseChannelScopeResponse,
  PreviewReleaseChannelScopeResponseSchema,
  type ReleaseChannel,
  type ReleaseChannelMiner,
  ReleaseChannelMinerSchema,
  type ReleaseChannelModelGroup,
  ReleaseChannelModelGroupSchema,
  ReleaseChannelSchema,
  ReleaseChannelScopeSchema,
  type Rollout,
  type RolloutBehavior,
  RolloutBehaviorSchema,
  RolloutCancelReason,
  type RolloutDevice,
  RolloutDeviceCountsSchema,
  RolloutDevicePhase,
  RolloutDeviceSchema,
  RolloutEvidenceSchema,
  RolloutMethod,
  RolloutOrder,
  RolloutSchema,
  RolloutStage,
  RolloutState,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";

// Fixture data for the release channel stories and tests: one mixed-fleet
// channel ("Canary") mid-update on its Rig group, plus the firmware files and
// display names the components take as props.
//
// The server pages channel members and rollout devices separately from the
// summaries. The full lists live here, keyed by channel / rollout id, and the
// summaries (counts, on-target, reported versions) are derived from them so
// the two can never disagree.

const minutesAgo = (minutes: number) => timestampFromDate(new Date(Date.now() - minutes * 60_000));

export const firmwareFiles: FirmwareFileInfo[] = [
  {
    id: "fw-rig-143",
    filename: "proto-rig-1.4.3.swu",
    size: 52_428_800,
    uploaded_at: "2026-08-20T09:00:00Z",
    target_manufacturer: "Proto",
    target_model: "Rig",
    firmware_version: "1.4.3",
  },
  {
    id: "fw-rig-144",
    filename: "proto-rig-1.4.4.swu",
    size: 52_690_944,
    uploaded_at: "2026-08-27T14:00:00Z",
    target_manufacturer: "Proto",
    target_model: "Rig",
    firmware_version: "1.4.4",
  },
  {
    id: "fw-s21-301",
    filename: "s21-3.0.1.bin",
    size: 33_554_432,
    uploaded_at: "2026-08-25T11:30:00Z",
    target_manufacturer: "Bitmain",
    target_model: "S21",
    firmware_version: "3.0.1",
  },
];

export const minerNames: Record<string, string> = {
  "rig-001": "Rig A01",
  "rig-002": "Rig A02",
  "rig-003": "Rig A03",
  "rig-004": "Rig B01",
  "rig-005": "Rig B02",
  "rig-006": "Rig B03",
  "s21-001": "S21 Rack 4-1",
  "s21-002": "S21 Rack 4-2",
};

export const singleBatchBehavior: RolloutBehavior = create(RolloutBehaviorSchema, {
  method: RolloutMethod.ALL_AT_ONCE,
  order: RolloutOrder.LEAST_EFFICIENT_FIRST,
});

export const pilotBehavior: RolloutBehavior = create(RolloutBehaviorSchema, {
  method: RolloutMethod.PILOT_THEN_CONTINUE,
  order: RolloutOrder.LEAST_EFFICIENT_FIRST,
  pilotSize: 2,
  reviewAfterEachBatch: true,
  maxConcurrentOffline: 10,
});

export const batchedAutoBehavior: RolloutBehavior = create(RolloutBehaviorSchema, {
  method: RolloutMethod.BATCHED,
  order: RolloutOrder.LEAST_EFFICIENT_FIRST,
  batchSize: 2,
  reviewAfterEachBatch: true,
  autoContinueOnHealthyTelemetry: true,
  stabilizationSeconds: 600,
  thresholds: { maxHashrateDropPercent: 10, maxTemperatureIncreaseCelsius: 5, maxNewErrors: 0 },
});

// ---------------------------------------------------------------------------
// Paged detail lists and the summaries derived from them
// ---------------------------------------------------------------------------

export const channelMiners: Record<string, ReleaseChannelMiner[]> = {};
export const rolloutDevices: Record<string, RolloutDevice[]> = {};

// Stand-ins for ReleaseChannelsApi.listChannelMiners / listRolloutDevices.
export const listChannelMinersFixture = (channelId: bigint, model?: string): Promise<ReleaseChannelMiner[]> =>
  Promise.resolve((channelMiners[channelId.toString()] ?? []).filter((m) => !model || m.model === model));
export const listRolloutDevicesFixture = (rolloutId: bigint): Promise<RolloutDevice[]> =>
  Promise.resolve(rolloutDevices[rolloutId.toString()] ?? []);

const rigMiner = (n: number, firmwareVersion: string): ReleaseChannelMiner =>
  create(ReleaseChannelMinerSchema, {
    deviceId: BigInt(100 + n),
    deviceIdentifier: `rig-00${n}`,
    model: "Rig",
    firmwareVersion,
  });

// A model group summary derived from its members, registered under the
// channel so listChannelMinersFixture can serve them.
const modelGroup = (
  channelId: bigint,
  fields: Pick<ReleaseChannelModelGroup, "model" | "firmwareFileId" | "firmwareVersion" | "activeRolloutId">,
  miners: ReleaseChannelMiner[],
): ReleaseChannelModelGroup => {
  const key = channelId.toString();
  channelMiners[key] = [...(channelMiners[key] ?? []).filter((m) => m.model !== fields.model), ...miners];
  return create(ReleaseChannelModelGroupSchema, {
    ...fields,
    minerCount: miners.length,
    onTargetCount: fields.firmwareVersion
      ? miners.filter((m) => m.firmwareVersion === fields.firmwareVersion).length
      : 0,
    reportedVersions: [...new Set(miners.map((m) => m.firmwareVersion))].sort(),
  });
};

const phaseCounts = (devices: RolloutDevice[]) => {
  const count = (phase: RolloutDevicePhase) => devices.filter((d) => d.phase === phase).length;
  return create(RolloutDeviceCountsSchema, {
    queued: count(RolloutDevicePhase.QUEUED),
    inProgress: count(RolloutDevicePhase.IN_PROGRESS),
    retrying: count(RolloutDevicePhase.RETRYING),
    done: count(RolloutDevicePhase.DONE),
    failed: count(RolloutDevicePhase.FAILED),
    excluded: count(RolloutDevicePhase.EXCLUDED),
  });
};

// A rollout summary derived from its devices, registered under the rollout id
// so listRolloutDevicesFixture can serve them. Batch counts follow the
// server: only outside the rest stage, for currentBatch (0-based) + 1.
const rolloutWithDevices = (fields: MessageInitShape<typeof RolloutSchema>, devices: RolloutDevice[]): Rollout => {
  const stage = fields.stage ?? RolloutStage.REST;
  const currentBatch = (fields.currentBatch ?? 0) + 1;
  const rollout = create(RolloutSchema, {
    ...fields,
    deviceCount: devices.length,
    deviceCounts: phaseCounts(devices),
    currentBatchCounts:
      stage === RolloutStage.REST
        ? create(RolloutDeviceCountsSchema)
        : phaseCounts(devices.filter((d) => d.batch === currentBatch)),
  });
  rolloutDevices[rollout.id.toString()] = devices;
  return rollout;
};

// ---------------------------------------------------------------------------
// Channels
// ---------------------------------------------------------------------------

// "Canary" applies to one rack plus two hand-picked S21s. Its Rig group is
// assigned 1.4.4 and partway through updating; S21 has no assignment yet.
export const canaryChannel: ReleaseChannel = create(ReleaseChannelSchema, {
  id: BigInt(1),
  name: "Canary",
  description: "First wave: rack 4 plus two S21s.",
  scope: { rackIds: [BigInt(40)], deviceIdentifiers: ["s21-001", "s21-002"] },
  behavior: pilotBehavior,
  minerCount: 8,
  createdAt: minutesAgo(60 * 24 * 3),
  updatedAt: minutesAgo(60 * 24),
  modelGroups: [
    modelGroup(
      BigInt(1),
      { model: "Rig", firmwareFileId: "fw-rig-144", firmwareVersion: "1.4.4", activeRolloutId: BigInt(41) },
      [
        rigMiner(1, "1.4.4"),
        rigMiner(2, "1.4.4"),
        rigMiner(3, "1.4.3"),
        rigMiner(4, "1.4.3"),
        rigMiner(5, "1.4.3"),
        rigMiner(6, "1.4.3"),
      ],
    ),
    modelGroup(BigInt(1), { model: "S21", firmwareFileId: "", firmwareVersion: "", activeRolloutId: BigInt(0) }, [
      create(ReleaseChannelMinerSchema, {
        deviceId: BigInt(201),
        deviceIdentifier: "s21-001",
        model: "S21",
        firmwareVersion: "3.0.0",
      }),
      create(ReleaseChannelMinerSchema, {
        deviceId: BigInt(202),
        deviceIdentifier: "s21-002",
        model: "S21",
        firmwareVersion: "3.0.0",
        conflicted: true,
      }),
    ]),
  ],
});

// The same channel with the Rig update finished and every miner compliant.
// Served under channel id 3 so productionChannel's members resolve too.
const settledRigGroup = modelGroup(
  BigInt(3),
  { model: "Rig", firmwareFileId: "fw-rig-144", firmwareVersion: "1.4.4", activeRolloutId: BigInt(0) },
  [1, 2, 3, 4, 5, 6].map((n) => rigMiner(n, "1.4.4")),
);
export const canaryChannelSettled: ReleaseChannel = create(ReleaseChannelSchema, {
  ...canaryChannel,
  modelGroups: canaryChannel.modelGroups.map((group) => (group.model === "Rig" ? settledRigGroup : group)),
});

export const emptyChannel: ReleaseChannel = create(ReleaseChannelSchema, {
  id: BigInt(2),
  name: "Staging",
  description: "",
  scope: {},
  behavior: singleBatchBehavior,
  minerCount: 0,
  createdAt: minutesAgo(30),
  updatedAt: minutesAgo(30),
  modelGroups: [],
});

// A settled channel with a distinct id, for stories listing several channels.
export const productionChannel: ReleaseChannel = create(ReleaseChannelSchema, {
  ...canaryChannelSettled,
  id: BigInt(3),
  name: "Production",
  description: "Everything else.",
  scope: create(ReleaseChannelScopeSchema, { siteIds: [BigInt(1)] }),
  behavior: batchedAutoBehavior,
  minerCount: 240,
});

export const canaryPreview: PreviewReleaseChannelScopeResponse = create(PreviewReleaseChannelScopeResponseSchema, {
  minerCount: 8,
  models: [
    { model: "Rig", minerCount: 6 },
    { model: "S21", minerCount: 2 },
  ],
});

export const conflictingPreview: PreviewReleaseChannelScopeResponse = create(PreviewReleaseChannelScopeResponseSchema, {
  minerCount: 8,
  models: [
    { model: "Rig", minerCount: 6 },
    { model: "S21", minerCount: 2 },
  ],
  conflicts: [{ channelId: BigInt(3), channelName: "Production", minerCount: 2 }],
});

// ---------------------------------------------------------------------------
// Rollouts
// ---------------------------------------------------------------------------

// Health fields for a miner as the server reports them: live status and
// telemetry alongside the baseline captured when the rollout started.
const TH = 1e12;
const metric = (baseline: number | undefined, current: number | undefined) =>
  create(MetricComparisonSchema, { baseline, current });
// Power, efficiency and temperature scale with hashrate so a degraded miner
// reads consistently across the strip.
const healthy = (hashRateTh: number, baselineTh = hashRateTh, ipSuffix = 11) => ({
  status: "ACTIVE",
  online: true,
  hashing: true,
  hasBaseline: true,
  baselineHashing: true,
  ipAddress: `10.20.4.${ipSuffix}`,
  hashRateHs: metric(baselineTh * TH, hashRateTh * TH),
  powerW: metric(baselineTh * 30, hashRateTh * 30 + 40),
  efficiencyJh: metric(30, hashRateTh > 0 ? 30 + 40 / hashRateTh : undefined),
  tempC: metric(64, hashRateTh >= baselineTh ? 65 : 71),
  openErrors: 0,
  baselineOpenErrors: 0,
});
const offline = (baselineTh: number, ipSuffix = 11) => ({
  ...healthy(0, baselineTh, ipSuffix),
  status: "OFFLINE",
  online: false,
  hashing: false,
  hashRateHs: metric(baselineTh * TH, undefined),
  powerW: metric(baselineTh * 30, undefined),
  efficiencyJh: metric(30, undefined),
  tempC: metric(64, undefined),
});

const rigDevice = (n: number, firmwareVersion: string, phase: RolloutDevicePhase, extra: object = {}) =>
  create(RolloutDeviceSchema, {
    deviceId: BigInt(100 + n),
    deviceIdentifier: `rig-00${n}`,
    firmwareVersion,
    phase,
    ...healthy(112, 112, 10 + n),
    ...extra,
  });

// Ongoing single-batch update to 1.4.4 on the Rig group: two miners done,
// one mid-flash, three queued.
export const activeRigRollout: Rollout = rolloutWithDevices(
  {
    id: BigInt(41),
    channelId: BigInt(1),
    channelName: "Canary",
    model: "Rig",
    firmwareFileId: "fw-rig-144",
    firmwareVersion: "1.4.4",
    status: RolloutStatus.ACTIVE,
    state: RolloutState.IN_PROGRESS,
    stage: RolloutStage.REST,
    behavior: singleBatchBehavior,
    stageChangedAt: minutesAgo(4),
    createdAt: minutesAgo(4),
    previousFirmwareFileId: "fw-rig-143",
    previousFirmwareVersion: "1.4.3",
    evidence: create(RolloutEvidenceSchema, {
      devicesTotal: 6,
      verified: 2,
      online: 5,
      hashing: 5,
      baselineHashing: 6,
      hashRateHs: metric(672 * TH, 560 * TH),
      hashrateChangePercent: -16.7,
      powerW: metric(560 * 30, 560 * 30 + 200),
      efficiencyJh: metric(30, 30.4),
      tempC: metric(64, 65),
    }),
  },
  [
    rigDevice(1, "1.4.4", RolloutDevicePhase.DONE, { attempts: 1 }),
    rigDevice(2, "1.4.4", RolloutDevicePhase.DONE, { attempts: 1 }),
    rigDevice(3, "1.4.3", RolloutDevicePhase.IN_PROGRESS, { attempts: 1, ...offline(112, 13) }),
    rigDevice(4, "1.4.3", RolloutDevicePhase.QUEUED),
    rigDevice(5, "1.4.3", RolloutDevicePhase.QUEUED),
    rigDevice(6, "1.4.3", RolloutDevicePhase.QUEUED),
  ],
);

// A pilot update parked at the review gate with healthy evidence: both pilot
// miners are back and hashing slightly above baseline.
export const gatedRigRollout: Rollout = rolloutWithDevices(
  {
    ...activeRigRollout,
    id: BigInt(45),
    state: RolloutState.PAUSED_AT_PILOT_GATE,
    stage: RolloutStage.AWAITING_REVIEW,
    behavior: pilotBehavior,
    batchCount: 1,
    currentBatch: 0,
    stageChangedAt: minutesAgo(6),
    createdAt: minutesAgo(22),
    evidence: create(RolloutEvidenceSchema, {
      devicesTotal: 2,
      verified: 2,
      online: 2,
      hashing: 2,
      baselineHashing: 2,
      hashRateHs: metric(224 * TH, 230 * TH),
      hashrateChangePercent: 2.7,
      holdReason: "Manual review",
      powerW: metric(224 * 30, 230 * 30 + 80),
      efficiencyJh: metric(30, 30.3),
      tempC: metric(64, 65),
    }),
  },
  [
    rigDevice(1, "1.4.4", RolloutDevicePhase.DONE, { batch: 1, attempts: 1, ...healthy(115, 112, 11) }),
    rigDevice(2, "1.4.4", RolloutDevicePhase.DONE, { batch: 1, attempts: 1, ...healthy(115, 112, 12) }),
    rigDevice(3, "1.4.3", RolloutDevicePhase.QUEUED),
    rigDevice(4, "1.4.3", RolloutDevicePhase.QUEUED),
    rigDevice(5, "1.4.3", RolloutDevicePhase.QUEUED),
    rigDevice(6, "1.4.3", RolloutDevicePhase.QUEUED),
  ],
);

// Batches of two with auto-continue, holding at the gate after batch 2 of 3
// because one miner failed and another opened errors.
export const batchedRigRollout: Rollout = rolloutWithDevices(
  {
    ...activeRigRollout,
    id: BigInt(46),
    state: RolloutState.PAUSED_AT_BATCH_REVIEW,
    stage: RolloutStage.AWAITING_REVIEW,
    behavior: batchedAutoBehavior,
    batchCount: 3,
    currentBatch: 1,
    stageChangedAt: minutesAgo(14),
    createdAt: minutesAgo(41),
    evidence: create(RolloutEvidenceSchema, {
      devicesTotal: 2,
      verified: 1,
      failed: 1,
      online: 1,
      hashing: 1,
      baselineHashing: 2,
      hashRateHs: metric(112 * TH, 112 * TH),
      newErrors: 2,
      holdReason: "1 miners failed to update",
      powerW: metric(112 * 30, 112 * 30 + 40),
      efficiencyJh: metric(30, 30.4),
      tempC: metric(64, 65),
    }),
  },
  [
    rigDevice(1, "1.4.4", RolloutDevicePhase.DONE, { batch: 1, attempts: 1 }),
    rigDevice(2, "1.4.4", RolloutDevicePhase.DONE, { batch: 1, attempts: 1 }),
    rigDevice(3, "1.4.4", RolloutDevicePhase.DONE, { batch: 2, attempts: 1, openErrors: 2 }),
    rigDevice(4, "1.4.3", RolloutDevicePhase.FAILED, {
      batch: 2,
      attempts: 3,
      lastError: "Did not report 1.4.4 after 3 update attempts",
      ...offline(112, 14),
    }),
    rigDevice(5, "1.4.3", RolloutDevicePhase.QUEUED, { batch: 3 }),
    rigDevice(6, "1.4.3", RolloutDevicePhase.QUEUED, { batch: 3 }),
  ],
);

// A single-batch update an operator paused mid-flight.
export const pausedRigRollout: Rollout = rolloutWithDevices(
  {
    ...activeRigRollout,
    id: BigInt(47),
    state: RolloutState.PAUSED,
    pausedAt: minutesAgo(3),
    createdAt: minutesAgo(11),
    evidence: create(RolloutEvidenceSchema, {
      ...(activeRigRollout.evidence ?? create(RolloutEvidenceSchema)),
      holdReason: "Paused",
    }),
  },
  rolloutDevices[activeRigRollout.id.toString()] ?? [],
);

// The first update the channel ran: 1.4.3 onto factory firmware, so there is
// nothing before it to roll back to.
export const completedRigRollout: Rollout = rolloutWithDevices(
  {
    id: BigInt(40),
    channelId: BigInt(1),
    channelName: "Canary",
    model: "Rig",
    firmwareFileId: "fw-rig-143",
    firmwareVersion: "1.4.3",
    status: RolloutStatus.COMPLETED,
    state: RolloutState.COMPLETED,
    stage: RolloutStage.REST,
    behavior: singleBatchBehavior,
    createdAt: minutesAgo(60 * 26),
    finishedAt: minutesAgo(60 * 25),
  },
  [1, 2, 3, 4, 5, 6].map((n) => rigDevice(n, "1.4.3", RolloutDevicePhase.DONE, { attempts: 1 })),
);

export const completedWithFailuresRigRollout: Rollout = rolloutWithDevices(
  {
    ...completedRigRollout,
    id: BigInt(48),
    firmwareFileId: "fw-rig-144",
    firmwareVersion: "1.4.4",
    status: RolloutStatus.COMPLETED_WITH_FAILURES,
    state: RolloutState.COMPLETED_WITH_FAILURES,
    previousFirmwareFileId: "fw-rig-143",
    previousFirmwareVersion: "1.4.3",
    createdAt: minutesAgo(60 * 3),
    finishedAt: minutesAgo(60 * 2),
  },
  [
    ...[1, 2, 3, 4, 5].map((n) => rigDevice(n, "1.4.4", RolloutDevicePhase.DONE, { attempts: 1 })),
    rigDevice(6, "1.4.3", RolloutDevicePhase.FAILED, {
      attempts: 3,
      lastError: "Did not report 1.4.4 after 3 update attempts",
      ...offline(112, 16),
    }),
  ],
);

export const canceledRigRollout: Rollout = rolloutWithDevices(
  {
    id: BigInt(39),
    channelId: BigInt(1),
    channelName: "Canary",
    model: "Rig",
    firmwareFileId: "fw-rig-142",
    firmwareVersion: "1.4.2",
    status: RolloutStatus.CANCELED,
    state: RolloutState.CANCELED,
    cancelReason: RolloutCancelReason.SUPERSEDED,
    behavior: singleBatchBehavior,
    createdAt: minutesAgo(60 * 27),
    finishedAt: minutesAgo(60 * 26),
  },
  [],
);

// A pilot update an operator canceled at the review gate: the pilot miners
// keep 1.4.4, the rest were never touched.
export const canceledRemainingRigRollout: Rollout = rolloutWithDevices(
  {
    ...gatedRigRollout,
    id: BigInt(38),
    status: RolloutStatus.CANCELED,
    state: RolloutState.CANCELED,
    cancelReason: RolloutCancelReason.CANCELED_REMAINING,
    previousFirmwareFileId: "fw-rig-143",
    previousFirmwareVersion: "1.4.3",
    createdAt: minutesAgo(60 * 48),
    finishedAt: minutesAgo(60 * 47),
    evidence: undefined,
  },
  rolloutDevices[gatedRigRollout.id.toString()] ?? [],
);

// Newest first, matching the server's ListRollouts order.
export const canaryHistory: Rollout[] = [
  activeRigRollout,
  completedWithFailuresRigRollout,
  completedRigRollout,
  canceledRigRollout,
  canceledRemainingRigRollout,
];
