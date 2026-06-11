import type { Meta, StoryObj } from "@storybook/react";

import { CurtailmentSettingsContent } from "./CurtailmentSettingsPage";
import type { CurtailmentSource, ResponseProfile } from "./types";

import { withMockedMinerSelectionApis } from "@/protoFleet/stories/MockedMinerSelectionApis";
import { formatTimestamp, isoToEpochSeconds } from "@/shared/utils/formatTimestamp";

const formatStorySignalUpdate = (isoString: string): string =>
  formatTimestamp(isoToEpochSeconds(isoString), { includeSeconds: true });

const storySources: CurtailmentSource[] = [
  {
    id: "site-alpha-mqtt",
    name: "Site Alpha MQTT",
    triggerType: "MQTT",
    brokerHosts: ["site-alpha-primary.broker.test", "site-alpha-secondary.broker.test"],
    port: 11883,
    topic: "curtailment/site-alpha/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "curtailment-alpha",
    lastTarget: "0",
    lastSeen: formatStorySignalUpdate("2026-06-09T15:10:00Z"),
    health: "connected",
    enabled: true,
  },
  {
    id: "site-beta-mqtt",
    name: "Site Beta MQTT",
    triggerType: "MQTT",
    brokerHosts: ["site-beta-primary.broker.test", "site-beta-secondary.broker.test"],
    port: 11884,
    topic: "curtailment/site-beta/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "curtailment-beta",
    lastTarget: "100",
    lastSeen: formatStorySignalUpdate("2026-06-09T15:10:30Z"),
    health: "connected",
    enabled: true,
  },
  {
    id: "site-gamma-mqtt",
    name: "Site Gamma MQTT",
    triggerType: "MQTT",
    brokerHosts: ["site-gamma-primary.broker.test", "site-gamma-secondary.broker.test"],
    port: 11885,
    topic: "curtailment/site-gamma/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "curtailment-gamma",
    lastTarget: "0",
    lastSeen: formatStorySignalUpdate("2026-06-09T14:58:00Z"),
    health: "noSignal",
    enabled: true,
  },
  {
    id: "site-delta-mqtt",
    name: "Site Delta MQTT",
    triggerType: "MQTT",
    brokerHosts: ["site-delta-primary.broker.test", "site-delta-secondary.broker.test"],
    port: 11886,
    topic: "curtailment/site-delta/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "curtailment-delta",
    lastTarget: "-",
    lastSeen: "-",
    health: "waitingForSignal",
    enabled: true,
  },
];

const storyResponseProfiles: ResponseProfile[] = [
  {
    id: "standard-shed",
    name: "Standard shed",
    targetSummary: "50% reduction",
    siteId: "101",
    scope: "All sites",
    selectionStrategy: "Least efficient first",
    restoreBehavior: "Restore in batches",
    deadlineSummary: "Within 15 min",
  },
  {
    id: "emergency-shed",
    name: "Emergency shed",
    targetSummary: "100% reduction",
    siteId: "",
    scope: "Whole fleet",
    selectionStrategy: "Least efficient first",
    restoreBehavior: "Restore immediately",
    deadlineSummary: "Within 15 min",
    formValues: {
      name: "Emergency shed",
      actionType: "fullFleet",
      targetKw: "",
      deviceIdentifiers: [],
      siteId: "",
      siteName: "",
      selectionStrategy: "leastEfficientFirst",
      restoreBehavior: "automaticImmediateRestore",
      minDurationSec: "",
      maxDurationSec: "900",
      curtailBatchSize: "50",
      curtailBatchIntervalSec: "30",
      restoreBatchSize: "10000",
      restoreIntervalSec: "0",
      responseDeadlineMinutes: "15",
      includeMaintenance: false,
    },
  },
  {
    id: "partial-reduction",
    name: "Partial reduction",
    targetSummary: "2,000 kW target",
    siteId: "101",
    scope: "Austin, TX",
    selectionStrategy: "Least efficient first",
    restoreBehavior: "Restore in batches",
    deadlineSummary: "Within 15 min",
    formValues: {
      name: "Partial reduction",
      actionType: "fixedKwReduction",
      targetKw: "2000",
      deviceIdentifiers: [],
      siteId: "101",
      siteName: "Austin, TX",
      selectionStrategy: "leastEfficientFirst",
      restoreBehavior: "automaticBatchRestore",
      minDurationSec: "",
      maxDurationSec: "900",
      curtailBatchSize: "50",
      curtailBatchIntervalSec: "30",
      restoreBatchSize: "",
      restoreIntervalSec: "",
      responseDeadlineMinutes: "15",
      includeMaintenance: false,
    },
  },
  {
    id: "miner-subset-shed",
    name: "Miner subset shed",
    targetSummary: "750 kW target",
    siteId: "",
    scope: "12 miners",
    selectionStrategy: "Least efficient first",
    restoreBehavior: "Restore in batches",
    deadlineSummary: "Within 15 min",
    formValues: {
      name: "Miner subset shed",
      actionType: "fixedKwReduction",
      targetKw: "750",
      deviceIdentifiers: [
        "miner-001",
        "miner-002",
        "miner-003",
        "miner-004",
        "miner-005",
        "miner-006",
        "miner-007",
        "miner-008",
        "miner-009",
        "miner-010",
        "miner-011",
        "miner-012",
      ],
      siteId: "",
      siteName: "",
      selectionStrategy: "leastEfficientFirst",
      restoreBehavior: "automaticBatchRestore",
      minDurationSec: "",
      maxDurationSec: "900",
      curtailBatchSize: "25",
      curtailBatchIntervalSec: "45",
      restoreBatchSize: "10",
      restoreIntervalSec: "120",
      responseDeadlineMinutes: "15",
      includeMaintenance: false,
    },
  },
];

const meta = {
  title: "Proto Fleet/Settings/Curtailment",
  component: CurtailmentSettingsContent,
  render: (args) => {
    const sourcesKey = args.initialSources?.map((source) => source.id).join(":") ?? "empty";
    const responseProfilesKey = args.initialResponseProfiles?.map((profile) => profile.id).join(":") ?? "empty";

    return (
      <div className="min-h-screen bg-surface-base p-10 phone:p-6">
        <CurtailmentSettingsContent
          key={[
            responseProfilesKey,
            sourcesKey,
            String(args.initialResponseProfileModalOpen),
            String(args.initialSourceModalOpen),
          ].join("-")}
          {...args}
        />
      </div>
    );
  },
  parameters: {
    layout: "fullscreen",
  },
  decorators: [withMockedMinerSelectionApis],
  tags: ["autodocs"],
} satisfies Meta<typeof CurtailmentSettingsContent>;

export default meta;

type Story = StoryObj<typeof meta>;

export const SettingsPage: Story = {
  args: {
    initialResponseProfiles: storyResponseProfiles,
    initialSources: storySources,
  },
};

export const EmptyState: Story = {};

export const AddSourceDialog: Story = {
  args: {
    initialResponseProfiles: storyResponseProfiles,
    initialSources: storySources,
    initialSourceModalOpen: true,
  },
};

export const AddResponseProfileDialog: Story = {
  args: {
    initialResponseProfiles: storyResponseProfiles,
    initialSources: storySources,
    initialResponseProfileModalOpen: true,
    onTestResponseProfileCurtailment: async () => undefined,
  },
};
