import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { batchedFirmwareConfig } from "@/protoFleet/features/rollout/rollout.fixtures";
import RolloutColumnState from "@/protoFleet/features/rollout/RolloutColumnState";
import RolloutControls from "@/protoFleet/features/rollout/RolloutControls";
import type { RolloutPlanConfig, RolloutTargetPhase } from "@/protoFleet/features/rollout/rolloutTypes";

/**
 * The abstracted, isolated rollout components — shown out of context so their
 * own behavior is easy to inspect. The in-situ stories cover how they read on
 * real surfaces; this bucket is just the parts.
 */
const meta = {
  title: "Proto Fleet/Rollout/Components",
  parameters: {
    layout: "centered",
  },
} satisfies Meta;

export default meta;

type Story = StoryObj;

// ---- Rollout controls: one functional example (flip the strategy live) ------
// RolloutControls owns no state, so the story holds it. The strategy selector
// is inside the component, so a single instance covers every strategy's
// field-set — pick a strategy to watch the paced fields appear/disappear.

function RolloutControlsStory(): ReactElement {
  const [config, setConfig] = useState<RolloutPlanConfig>(batchedFirmwareConfig);
  return (
    <div className="w-[520px] max-w-full rounded-xl bg-surface-elevated-base p-8 shadow-100">
      <RolloutControls config={config} onChange={setConfig} />
    </div>
  );
}

export const Controls: Story = {
  name: "Rollout controls",
  render: () => <RolloutControlsStory />,
};

// ---- Rollout column state: every phase as it reads in the fleet table -------

interface ColumnRow {
  miner: string;
  phase: RolloutTargetPhase;
  doneLabel?: string;
  idleLabel?: string;
}

const columnRows: ColumnRow[] = [
  { miner: "M-1042", phase: "done", doneLabel: "5.1.0" },
  { miner: "M-1043", phase: "inProgress" },
  { miner: "M-1044", phase: "retrying" },
  { miner: "M-1045", phase: "failed" },
  { miner: "M-1046", phase: "queued", idleLabel: "5.0.2" },
  { miner: "M-1047", phase: "excluded" },
];

export const ColumnState: Story = {
  name: "Rollout column state",
  render: () => (
    <div className="w-[420px] max-w-full overflow-hidden rounded-xl border border-border-5 bg-surface-elevated-base">
      <div className="grid grid-cols-[1fr_auto] gap-4 border-b border-border-5 px-4 py-2.5 text-200 text-text-primary-50">
        <span>Miner</span>
        <span>Firmware</span>
      </div>
      {columnRows.map((row) => (
        <div
          key={row.miner}
          className="grid grid-cols-[1fr_auto] items-center gap-4 border-b border-border-5 px-4 py-3 last:border-b-0"
        >
          <span className="text-300 text-text-primary">{row.miner}</span>
          <RolloutColumnState phase={row.phase} doneLabel={row.doneLabel} idleLabel={row.idleLabel} />
        </div>
      ))}
    </div>
  ),
};
