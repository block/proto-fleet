import type { Meta, StoryObj } from "@storybook/react-vite";

import { batchedRigRollout, gatedRigRollout, minerNames } from "./RolloutChannels.fixtures";
import RolloutMinersModal from "./RolloutMinersModal";

// The miner drill-down behind "View miners" / "Review miners": every
// targeted miner with its update state and telemetry against baseline, or
// just the miners that need attention.
const meta = {
  title: "Proto Fleet/Firmware/Rollout Channels/Update Miners",
  component: RolloutMinersModal,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof RolloutMinersModal>;

export default meta;

type Story = StoryObj<typeof RolloutMinersModal>;

const noop = () => {};

export const AllMiners: Story = {
  name: "All miners",
  render: () => <RolloutMinersModal rollout={gatedRigRollout} minerNames={minerNames} onClose={noop} />,
};

export const NeedsAttention: Story = {
  name: "Miners needing attention",
  render: () => (
    <RolloutMinersModal rollout={batchedRigRollout} minerNames={minerNames} initialFilter="attention" onClose={noop} />
  ),
};
