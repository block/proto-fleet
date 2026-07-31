import type { Meta, StoryObj } from "@storybook/react-vite";

import { ActiveRolloutBanner, ActiveRolloutBannerStack } from "@/protoFleet/features/rollout/ActiveRolloutBanner";
import {
  inProgressCurtailmentEvent,
  inProgressFirmwareEvent,
  inProgressRebootEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";

const meta = {
  title: "Proto Fleet/Rollout/Active Rollout Banner",
  component: ActiveRolloutBanner,
  parameters: {
    layout: "fullscreen",
  },
  decorators: [
    (Story) => (
      <div className="min-h-screen bg-surface-base p-8">
        <div className="mx-auto max-w-4xl">
          <Story />
        </div>
      </div>
    ),
  ],
} satisfies Meta<typeof ActiveRolloutBanner>;

export default meta;

type Story = StoryObj<typeof ActiveRolloutBanner>;

const noop = () => undefined;

export const SingleFirmware: Story = {
  name: "Single banner — firmware",
  args: {
    event: inProgressFirmwareEvent,
    onView: noop,
  },
};

export const Stacked: Story = {
  name: "Stacked — Active now (3 processes)",
  render: () => (
    <div className="grid gap-3">
      <div className="flex items-center justify-between gap-4">
        <div className="text-emphasis-300 text-text-primary">Active now</div>
        <span className="text-200 text-text-primary-70">3 processes running</span>
      </div>
      <ActiveRolloutBannerStack
        events={[inProgressFirmwareEvent, inProgressRebootEvent, inProgressCurtailmentEvent]}
        selectedIndex={0}
        onView={noop}
      />
    </div>
  ),
};
