import type { Meta, StoryObj } from "@storybook/react-vite";
import { fn } from "storybook/test";

import { activeRigRollout, batchedRigRollout, gatedRigRollout, pausedRigRollout } from "./ReleaseChannels.fixtures";
import RolloutLiveView from "./RolloutLiveView";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

const meta = {
  title: "Proto Fleet/Firmware/Release Channels/Active Update Card",
  component: RolloutLiveView,
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "Fixed server snapshots. Lifecycle and management callbacks are recorded in Actions without changing fixture data. Use the Active Monitor stories for navigation and confirmation flows.",
      },
    },
  },
  decorators: [
    (Story) => (
      <div className="min-h-screen bg-surface-base p-6 tablet:p-10">
        <Story />
      </div>
    ),
  ],
  argTypes: {
    rollout: { control: false },
    currentGeneration: { control: false },
    onViewUpdate: { control: false },
  },
  args: {
    presentation: "inline",
    onContinue: fn(() => Promise.resolve()),
    onPause: fn(() => Promise.resolve()),
    onResume: fn(() => Promise.resolve()),
    onCancel: fn(),
    onRollback: fn(),
    onRetryFailed: fn(),
    onViewMiners: fn(),
    onManage: fn(),
    // Explicit absence also prevents the global action regex adding a callback.
    onViewUpdate: undefined,
    onClose: fn(),
  },
} satisfies Meta<typeof RolloutLiveView>;

export default meta;
type Story = StoryObj<typeof RolloutLiveView>;

// Protobuf fixtures contain bigint values, which Storybook's object controls
// cannot serialize. Keep them in the render while exposing primitive controls.
const card = (rollout: Rollout): Story => ({
  render: (args) => <RolloutLiveView {...args} rollout={rollout} currentGeneration={rollout.assignmentGeneration} />,
});

export const Updating: Story = card(activeRigRollout);
export const PilotReview: Story = card(gatedRigRollout);
export const HoldingForReview: Story = card(batchedRigRollout);
export const Paused: Story = card(pausedRigRollout);
