import type { ComponentProps } from "react";
import type { Meta, StoryObj } from "@storybook/react";

import FleetContextualSuggestion from "./FleetContextualSuggestion";
import { Asic, Building, Fleet, Racks } from "@/shared/assets/icons";

const meta = {
  title: "Proto Fleet/Fleet Management/FleetContextualSuggestion",
  component: FleetContextualSuggestion,
  parameters: {
    layout: "padded",
  },
} satisfies Meta<typeof FleetContextualSuggestion>;

export default meta;
type Story = StoryObj<typeof meta>;
type FleetContextualSuggestionProps = ComponentProps<typeof FleetContextualSuggestion>;

const noop = () => undefined;

const rackCreationArgs: FleetContextualSuggestionProps = {
  icon: <Racks width="w-4" />,
  title: "24 unassigned miners from 10.90.12.40-10.90.12.63 look like a rack.",
  detail: "Seen in nearby IPs. Mostly Proto Rig.",
  action: {
    label: "Review",
    onClick: noop,
  },
  onDismiss: noop,
};

const minerPairingArgs: FleetContextualSuggestionProps = {
  icon: <Asic width="w-4" />,
  title: "12 detected miners in 10.90.13.80-10.90.13.91 are ready to pair.",
  detail: "Found during the latest network poll. All are reporting default credentials.",
  action: {
    label: "Review",
    onClick: noop,
  },
  onDismiss: noop,
};

const containerCreationArgs: FleetContextualSuggestionProps = {
  icon: <Fleet width="w-4" />,
  title: "96 miners across 10.90.20.0/24 look like a container.",
  detail: "Detected as four adjacent rack-sized cohorts with matching firmware and model.",
  action: {
    label: "Review",
    onClick: noop,
  },
  onDismiss: noop,
};

const buildingGroupingArgs: FleetContextualSuggestionProps = {
  icon: <Building width="w-4" />,
  title: "4 rack-shaped cohorts in 10.90.0.0/20 look like Building B.",
  detail: "Detected from configured IP ranges and contiguous rack groupings.",
  action: {
    label: "Review",
    onClick: noop,
  },
  onDismiss: noop,
};

const examples = [rackCreationArgs, minerPairingArgs, containerCreationArgs, buildingGroupingArgs];

export const AllExamples: Story = {
  args: rackCreationArgs,
  render: () => (
    <div className="flex max-w-5xl flex-col gap-3">
      {examples.map((example) => (
        <FleetContextualSuggestion key={example.title} {...example} />
      ))}
    </div>
  ),
};

export const RackCreation: Story = {
  args: rackCreationArgs,
};

export const MinerPairing: Story = {
  args: minerPairingArgs,
};

export const ContainerCreation: Story = {
  args: containerCreationArgs,
};

export const BuildingGrouping: Story = {
  args: buildingGroupingArgs,
};
