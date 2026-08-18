import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  abortedRollout,
  attentionRequiredRollout,
  betweenChannelFiles,
  stableProductionLane,
} from "./betweenChannel.fixtures";
import BetweenChannelRolloutStatus from "./BetweenChannelRolloutStatus";
import StartRolloutLaneModal from "./StartRolloutLaneModal";

const noop = (): void => undefined;

const meta = {
  title: "Proto Fleet/Rollout/Between Channel",
  component: BetweenChannelRolloutStatus,
  parameters: {
    layout: "fullscreen",
  },
  decorators: [
    (Story) => (
      <div className="min-h-screen bg-surface-base p-6 tablet:p-10">
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof BetweenChannelRolloutStatus>;

export default meta;
type Story = StoryObj<typeof meta>;

export const AttentionRequired: Story = {
  args: {
    rollout: attentionRequiredRollout,
    laneLabel: stableProductionLane.label,
    canControl: true,
    onPause: noop,
    onAbort: noop,
  },
};

export const AbortedAndRevertEligible: Story = {
  args: {
    rollout: abortedRollout,
    laneLabel: stableProductionLane.label,
    canControl: true,
    onRevert: noop,
  },
};

export const StartFlow: Story = {
  args: {
    rollout: attentionRequiredRollout,
    laneLabel: stableProductionLane.label,
    canControl: true,
  },
  render: () => (
    <StartRolloutLaneModal
      open
      lane={stableProductionLane}
      files={betweenChannelFiles}
      isSubmitting={false}
      onDismiss={noop}
      onStart={noop}
    />
  ),
};
