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
  {
    id: "dev",
    name: "Dev",
    minerCount: 240,
    releaseCount: 3,
    lastUpdated: "11/7/25 9:01 PM",
    updateStatus: "2 active, 1 needs attention",
    updateTone: "attention",
    modelCohorts: [
      {
        id: "dev-s21-pro",
        model: "Antminer S21 Pro",
        minerCount: 90,
        previousVersion: "2.1.0",
        currentVersion: "2.2.1",
        updateStatus: "Needs attention",
        updateTone: "attention",
      },
      {
        id: "dev-s21",
        model: "Antminer S21",
        minerCount: 80,
        previousVersion: "1.9.8",
        currentVersion: "2.0.4",
        updateStatus: "Updating, 54 of 78",
        updateTone: "active",
      },
      {
        id: "dev-m60",
        model: "Whatsminer M60",
        minerCount: 70,
        previousVersion: "2024.10.1",
        currentVersion: "2025.02.3",
        updateStatus: "Completed, 66 of 66",
        updateTone: "completed",
      },
    ],
  },
  {
    id: "staging",
    name: "Staging",
    minerCount: 2789,
    releaseCount: 3,
    lastUpdated: "11/7/25 9:01 PM",
    updateStatus: "No active updates",
    updateTone: "none",
    modelCohorts: [
      {
        id: "staging-s21-pro",
        model: "Antminer S21 Pro",
        minerCount: 914,
        previousVersion: "2.1.0",
        currentVersion: "2.2.1",
        updateStatus: "Updated Nov 7, 2025",
        updateTone: "completed",
      },
      {
        id: "staging-s21",
        model: "Antminer S21",
        minerCount: 1120,
        previousVersion: "1.9.8",
        currentVersion: "2.0.4",
        updateStatus: "Updated Nov 7, 2025",
        updateTone: "completed",
      },
      {
        id: "staging-m60",
        model: "Whatsminer M60",
        minerCount: 755,
        previousVersion: "2024.10.1",
        currentVersion: "2025.02.3",
        updateStatus: "Updated Nov 6, 2025",
        updateTone: "completed",
      },
    ],
  },
  {
    id: "production",
    name: "Production",
    minerCount: 10345,
    releaseCount: 3,
    lastUpdated: "11/7/25 9:01 PM",
    updateStatus: "1 active",
    updateTone: "active",
    modelCohorts: [
      {
        id: "production-s21-pro",
        model: "Antminer S21 Pro",
        minerCount: 3810,
        previousVersion: "2.1.0",
        currentVersion: "2.2.1",
        updateStatus: "Queued",
        updateTone: "active",
      },
      {
        id: "production-s21",
        model: "Antminer S21",
        minerCount: 3540,
        previousVersion: "1.9.8",
        currentVersion: "2.0.4",
        updateStatus: "Updated Nov 7, 2025",
        updateTone: "completed",
      },
      {
        id: "production-m60",
        model: "Whatsminer M60",
        minerCount: 2995,
        previousVersion: "2024.10.1",
        currentVersion: "2025.02.3",
        updateStatus: "Updating, 1,482 of 2,995",
        updateTone: "active",
      },
    ],
  },
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
