import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { defaultBehavior } from "./behaviorUtils";
import { batchedAutoBehavior, pilotBehavior } from "./ReleaseChannels.fixtures";
import RolloutControls from "./RolloutControls";
import type { RolloutBehavior } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

// The "Update behavior" controls. Flip the method live to see which fields
// each method exposes; the plan readout tracks the miners in scope.
const meta = {
  title: "Proto Fleet/Firmware/Release Channels/Update Behavior",
  component: RolloutControls,
  parameters: {
    layout: "padded",
  },
} satisfies Meta<typeof RolloutControls>;

export default meta;

type Story = StoryObj<typeof RolloutControls>;

const Live = ({ initial, inScopeCount }: { initial: RolloutBehavior; inScopeCount: number }) => {
  const [behavior, setBehavior] = useState(initial);
  return (
    <div className="max-w-2xl">
      <RolloutControls behavior={behavior} onChange={setBehavior} inScopeCount={inScopeCount} />
    </div>
  );
};

export const SingleBatch: Story = {
  render: () => <Live initial={defaultBehavior()} inScopeCount={48} />,
};

export const Pilot: Story = {
  name: "Pilot batch, then remaining",
  render: () => <Live initial={pilotBehavior} inScopeCount={48} />,
};

export const BatchesWithAutoContinue: Story = {
  name: "Multiple batches, auto-continue",
  render: () => <Live initial={batchedAutoBehavior} inScopeCount={48} />,
};
