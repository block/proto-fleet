import type {
  ReleaseChannel,
  ReleaseChannelDraft,
  ReleaseChannelFile,
  ReleaseChannelPreview,
  ReleaseChannelScope,
} from "./releaseChannelTypes";
import type { RolloutPlanConfig } from "./rolloutTypes";

/** The three tracks shown in the Release channels table (matching the mock). */
export const releaseChannels: ReleaseChannel[] = [
  { id: "dev", name: "Dev", minerCount: 100, releaseCount: 3, lastUpdated: "11/7/25 9:01 PM" },
  { id: "staging", name: "Staging", minerCount: 2789, releaseCount: 3, lastUpdated: "11/7/25 9:01 PM" },
  { id: "production", name: "Production", minerCount: 10345, releaseCount: 3, lastUpdated: "11/7/25 9:01 PM" },
];

const devChannelFiles: ReleaseChannelFile[] = [
  { id: "s21", model: "Antminer S21", file: "miner-image-release-123", uploaded: "11/7/25 9:01 PM" },
  { id: "s19xp", model: "Antminer S19 XP", file: "miner-image-release-456", uploaded: "11/7/25 9:01 PM" },
  { id: "m50s", model: "Whatsminer M50S", file: "miner-image-release-789", uploaded: "11/7/25 9:01 PM" },
];

const devChannelScope: ReleaseChannelScope = {
  sites: 1,
  buildings: 8,
  racks: 40,
  groups: 0,
  miners: 240,
};

/** Batched firmware pacing for a channel, the mock's Rollout section values. */
const devChannelConfig: RolloutPlanConfig = {
  processType: "firmware",
  strategy: "batched",
  order: "leastEfficientFirst",
  maxConcurrentOffline: 50,
  batchSize: 20,
  batchIntervalSec: 60,
  scheduleType: "startNow",
};

/** The coverage preview shown in the modal's right pane for the Dev channel. */
export const devChannelPreview: ReleaseChannelPreview = {
  minerCount: 240,
  modelCount: 3,
  siteCount: 1,
  buildingCount: 8,
  rackCount: 40,
  previousRollouts: [
    "Wednesday, Apr 22, 2026 at 6:00 AM",
    "Thursday, Apr 23, 2026 at 6:00 AM",
    "Friday, Apr 24, 2026 at 6:00 AM",
  ],
};

/** The Dev channel opened in the Manage release channel modal (matches mock 2). */
export const devChannelDraft: ReleaseChannelDraft = {
  name: "Dev",
  description: "Used for testing",
  files: devChannelFiles,
  scope: devChannelScope,
  config: devChannelConfig,
};
