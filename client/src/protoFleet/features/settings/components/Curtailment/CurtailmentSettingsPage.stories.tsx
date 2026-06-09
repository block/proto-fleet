import type { Meta, StoryObj } from "@storybook/react";

import { CurtailmentSettingsContent } from "./CurtailmentSettingsPage";
import type { CurtailmentSource } from "./types";

import { formatTimestamp, isoToEpochSeconds } from "@/shared/utils/formatTimestamp";

const formatStorySignalUpdate = (isoString: string): string =>
  formatTimestamp(isoToEpochSeconds(isoString), { includeSeconds: true });

const storySources: CurtailmentSource[] = [
  {
    id: "site-alpha-mqtt",
    name: "Site Alpha MQTT",
    triggerType: "MQTT",
    site: "Site Alpha",
    brokerHosts: ["site-alpha-primary.broker.test", "site-alpha-secondary.broker.test"],
    port: 11883,
    topic: "curtailment/site-alpha/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "curtailment-alpha",
    scope: "Site Alpha",
    curtailmentMode: "Curtail entire site",
    lastTarget: "0",
    lastSeen: formatStorySignalUpdate("2026-06-09T15:10:00Z"),
    health: "connected",
    enabled: true,
  },
  {
    id: "site-beta-mqtt",
    name: "Site Beta MQTT",
    triggerType: "MQTT",
    site: "Site Beta",
    brokerHosts: ["site-beta-primary.broker.test", "site-beta-secondary.broker.test"],
    port: 11884,
    topic: "curtailment/site-beta/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "curtailment-beta",
    scope: "Site Beta",
    curtailmentMode: "Curtail entire site",
    lastTarget: "100",
    lastSeen: formatStorySignalUpdate("2026-06-09T15:10:30Z"),
    health: "connected",
    enabled: true,
  },
  {
    id: "site-gamma-mqtt",
    name: "Site Gamma MQTT",
    triggerType: "MQTT",
    site: "Site Gamma",
    brokerHosts: ["site-gamma-primary.broker.test", "site-gamma-secondary.broker.test"],
    port: 11885,
    topic: "curtailment/site-gamma/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "curtailment-gamma",
    scope: "Site Gamma",
    curtailmentMode: "Curtail entire site",
    lastTarget: "0",
    lastSeen: formatStorySignalUpdate("2026-06-09T14:58:00Z"),
    health: "noSignal",
    enabled: true,
  },
  {
    id: "site-delta-mqtt",
    name: "Site Delta MQTT",
    triggerType: "MQTT",
    site: "Site Delta",
    brokerHosts: ["site-delta-primary.broker.test", "site-delta-secondary.broker.test"],
    port: 11886,
    topic: "curtailment/site-delta/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "curtailment-delta",
    scope: "Site Delta",
    curtailmentMode: "Curtail entire site",
    lastTarget: "-",
    lastSeen: "-",
    health: "waitingForSignal",
    enabled: true,
  },
];

const meta = {
  title: "Proto Fleet/Settings/Curtailment",
  component: CurtailmentSettingsContent,
  render: (args) => {
    const sourcesKey = args.initialSources?.map((source) => source.id).join(":") ?? "empty";

    return (
      <div className="min-h-screen bg-surface-base p-10 phone:p-6">
        <CurtailmentSettingsContent key={`${sourcesKey}-${String(args.initialSourceModalOpen)}`} {...args} />
      </div>
    );
  },
  parameters: {
    layout: "fullscreen",
  },
  tags: ["autodocs"],
} satisfies Meta<typeof CurtailmentSettingsContent>;

export default meta;

type Story = StoryObj<typeof meta>;

export const SettingsPage: Story = {
  args: {
    initialSources: storySources,
  },
};

export const EmptyState: Story = {};

export const AddSourceDialog: Story = {
  args: {
    initialSources: storySources,
    initialSourceModalOpen: true,
  },
};
