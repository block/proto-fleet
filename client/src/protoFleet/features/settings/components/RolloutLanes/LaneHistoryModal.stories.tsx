import type { Meta, StoryObj } from "@storybook/react-vite";

import LaneHistoryModal from "./LaneHistoryModal";
import { canaryChannel, canaryHistory, emptyChannel } from "./RolloutChannels.fixtures";

// The per-channel rollout history: status, model, version, progress, and
// timestamps per entry. Entries whose firmware differs from the model's
// current assignment offer a "Roll back" action; the entry matching the
// current assignment does not.
const meta = {
  title: "Proto Fleet/Firmware/Rollout Channels/History Modal",
  component: LaneHistoryModal,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof LaneHistoryModal>;

export default meta;

type Story = StoryObj<typeof LaneHistoryModal>;

const noop = () => {};

export const WithRollbackTargets: Story = {
  name: "History with rollback targets",
  render: () => <LaneHistoryModal lane={canaryChannel} rollouts={canaryHistory} onRollback={noop} onClose={noop} />,
};

export const EmptyHistory: Story = {
  name: "No rollouts yet",
  render: () => <LaneHistoryModal lane={emptyChannel} rollouts={[]} onRollback={noop} onClose={noop} />,
};
