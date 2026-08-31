import type { Meta, StoryObj } from "@storybook/react-vite";

import ModelMinersModal from "./ModelMinersModal";
import { activeRigRollout, canaryChannel, canaryChannelSettled, minerNames } from "./RolloutChannels.fixtures";

// The per-model miner table behind the "View miners" button: miner, the
// firmware it currently reports, and status — the live rollout state while
// one is running, otherwise compliance against the assigned version.
const meta = {
  title: "Proto Fleet/Firmware/Rollout Channels/Miners Modal",
  component: ModelMinersModal,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof ModelMinersModal>;

export default meta;

type Story = StoryObj<typeof ModelMinersModal>;

const noop = () => {};
const rigGroup = canaryChannel.modelGroups[0];
const settledRigGroup = canaryChannelSettled.modelGroups[0];
const unassignedS21Group = canaryChannel.modelGroups[1];

export const DuringRollout: Story = {
  name: "During a rollout",
  render: () => (
    <ModelMinersModal
      laneName="Canary"
      group={rigGroup}
      activeRollout={activeRigRollout}
      minerNames={minerNames}
      onClose={noop}
    />
  ),
};

export const AllCompliant: Story = {
  name: "All miners on the assigned version",
  render: () => (
    <ModelMinersModal
      laneName="Canary"
      group={settledRigGroup}
      activeRollout={undefined}
      minerNames={minerNames}
      onClose={noop}
    />
  ),
};

export const NoAssignment: Story = {
  name: "No firmware assigned",
  render: () => (
    <ModelMinersModal
      laneName="Canary"
      group={unassignedS21Group}
      activeRollout={undefined}
      minerNames={minerNames}
      onClose={noop}
    />
  ),
};
