import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  abortedRigRollout,
  activeRigRollout,
  batchedRigRollout,
  completedRigRollout,
  gatedRigRollout,
  minerNames,
  pausedRigRollout,
  pilotRigRollout,
} from "./RolloutChannels.fixtures";
import RolloutDetailModal from "./RolloutDetailModal";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

// The full-screen update detail behind "View update": a sticky header
// carrying the lifecycle actions (Manage / Continue / Pause / Resume, with
// View miners and Cancel remaining in the overflow), then errors first, the
// status lockup, plan stat lockups, progress against plan, and the
// telemetry evidence strip. "View miners" opens the standalone miners list.
const meta = {
  title: "Proto Fleet/Firmware/Rollout Channels/Update Detail",
  component: RolloutDetailModal,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof RolloutDetailModal>;

export default meta;

type Story = StoryObj<typeof RolloutDetailModal>;

const noop = () => {};
const afterDelay = () => new Promise<void>((resolve) => setTimeout(resolve, 800));

const Render = ({ rollout }: { rollout: Rollout }) => (
  <RolloutDetailModal
    rollout={rollout}
    minerNames={minerNames}
    onClose={noop}
    onContinue={afterDelay}
    onPause={afterDelay}
    onResume={afterDelay}
    onAbort={noop}
    onManage={noop}
  />
);

export const InProgress: Story = {
  name: "Update in progress",
  render: () => <Render rollout={activeRigRollout} />,
};

export const PilotInProgress: Story = {
  name: "Pilot batch in progress",
  render: () => <Render rollout={pilotRigRollout} />,
};

export const PilotAwaitingReview: Story = {
  name: "Pilot batch review (healthy evidence)",
  render: () => <Render rollout={gatedRigRollout} />,
};

export const BatchesHoldingOnEvidence: Story = {
  name: "Batch 2 review holding (miners need attention)",
  render: () => <Render rollout={batchedRigRollout} />,
};

export const Paused: Story = {
  name: "Paused update",
  render: () => <Render rollout={pausedRigRollout} />,
};

export const Completed: Story = {
  name: "Completed update",
  render: () => <Render rollout={completedRigRollout} />,
};

export const Aborted: Story = {
  name: "Canceled update",
  render: () => <Render rollout={abortedRigRollout} />,
};
