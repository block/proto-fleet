import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  allAtOnceFirmwareConfig,
  batchedFirmwareConfig,
  pilotFirmwareConfig,
} from "@/protoFleet/features/rollout/rollout.fixtures";
import RolloutControls from "@/protoFleet/features/rollout/RolloutControls";
import type { RolloutPlanConfig } from "@/protoFleet/features/rollout/rolloutTypes";

const meta = {
  title: "Proto Fleet/Rollout/Rollout Controls",
  component: RolloutControls,
  parameters: {
    layout: "centered",
  },
  decorators: [
    (Story) => (
      <div className="w-[520px] max-w-full rounded-xl bg-surface-elevated-base p-8 shadow-100">
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof RolloutControls>;

export default meta;

type Story = StoryObj<typeof RolloutControls>;

/** Controlled wrapper: RolloutControls owns no state, so the story holds it and
 * lets you flip the strategy to watch the field-set change live. */
function ControlledRolloutControls({ initialConfig }: { initialConfig: RolloutPlanConfig }): ReactElement {
  const [config, setConfig] = useState<RolloutPlanConfig>(initialConfig);
  return <RolloutControls config={config} onChange={setConfig} />;
}

export const Batches: Story = {
  name: "Strategy: update in batches",
  render: () => <ControlledRolloutControls initialConfig={batchedFirmwareConfig} />,
};

export const AllAtOnce: Story = {
  name: "Strategy: all at once",
  render: () => <ControlledRolloutControls initialConfig={allAtOnceFirmwareConfig} />,
};

export const PilotThenContinue: Story = {
  name: "Strategy: pilot then continue",
  render: () => <ControlledRolloutControls initialConfig={pilotFirmwareConfig} />,
};
