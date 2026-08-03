import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { ActiveRolloutBanner, ActiveRolloutBannerStack } from "@/protoFleet/features/rollout/ActiveRolloutBanner";
import {
  inProgressCurtailmentEvent,
  inProgressFirmwareEvent,
  inProgressRebootEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";
import ViewRolloutModal from "@/protoFleet/features/rollout/ViewRolloutModal";

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

/** A single banner whose "View rollout" opens the progress modal in place. */
function SingleBannerStory(): ReactElement {
  const [open, setOpen] = useState(false);
  return (
    <>
      <ActiveRolloutBanner event={inProgressFirmwareEvent} onView={() => setOpen(true)} />
      <ViewRolloutModal
        event={open ? inProgressFirmwareEvent : null}
        onDismiss={() => setOpen(false)}
        onPause={noop}
        onCancelRemaining={noop}
      />
    </>
  );
}

export const SingleFirmware: Story = {
  name: "Single banner — firmware (opens the modal)",
  render: () => <SingleBannerStory />,
};

/** The Activity "Active now" stack; each banner opens its own rollout's modal. */
function StackedStory(): ReactElement {
  const events = [inProgressFirmwareEvent, inProgressRebootEvent, inProgressCurtailmentEvent];
  const [openIndex, setOpenIndex] = useState<number | null>(null);
  return (
    <div className="grid gap-3">
      <div className="flex items-center justify-between gap-4">
        <div className="text-emphasis-300 text-text-primary">Active now</div>
        <span className="text-200 text-text-primary-70">3 processes running</span>
      </div>
      <ActiveRolloutBannerStack events={events} onView={(_event, index) => setOpenIndex(index)} />
      <ViewRolloutModal
        event={openIndex === null ? null : events[openIndex]}
        onDismiss={() => setOpenIndex(null)}
        onPause={noop}
        onCancelRemaining={noop}
      />
    </div>
  );
}

export const Stacked: Story = {
  name: "Stacked — Active now (banners open the modal)",
  render: () => <StackedStory />,
};
