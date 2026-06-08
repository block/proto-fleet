import type { Meta, StoryObj } from "@storybook/react";

import { CurtailmentSettingsContent } from "./CurtailmentSettingsPage";
import type { CurtailmentSource } from "./types";

const storySources: CurtailmentSource[] = [
  {
    id: "kati-maestro",
    name: "Kati MaestroOS",
    triggerType: "MQTT",
    site: "Kati",
    brokerHosts: ["10.155.0.3", "10.155.0.4"],
    port: 1883,
    topic: "maestro/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "soluna-kati",
    scope: "Kati",
    curtailmentMode: "Curtail entire site",
    lastTarget: 0,
    lastSeen: "38 seconds ago",
    health: "connected",
    enabled: true,
  },
  {
    id: "dorothy-2-maestro",
    name: "Dorothy 2 MaestroOS",
    triggerType: "MQTT",
    site: "Dorothy 2",
    brokerHosts: ["10.144.0.3", "10.144.0.4"],
    port: 1883,
    topic: "maestro/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "soluna-dorothy",
    scope: "Dorothy 2",
    curtailmentMode: "Curtail entire site",
    lastTarget: 100,
    lastSeen: "24 seconds ago",
    health: "connected",
    enabled: true,
  },
  {
    id: "helena-maestro",
    name: "Helena MaestroOS",
    triggerType: "MQTT",
    site: "Helena",
    brokerHosts: ["10.188.0.3", "10.188.0.4"],
    port: 1883,
    topic: "maestro/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "soluna-helena",
    scope: "Helena",
    curtailmentMode: "Curtail entire site",
    lastTarget: 0,
    lastSeen: "12 minutes ago",
    health: "stale",
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
