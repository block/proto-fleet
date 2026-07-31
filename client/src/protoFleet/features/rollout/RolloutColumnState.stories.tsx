import type { Meta, StoryObj } from "@storybook/react-vite";

import RolloutColumnState from "@/protoFleet/features/rollout/RolloutColumnState";
import type { RolloutTargetPhase } from "@/protoFleet/features/rollout/rolloutTypes";

const meta = {
  title: "Proto Fleet/Rollout/Rollout Column State",
  component: RolloutColumnState,
  parameters: {
    layout: "centered",
  },
} satisfies Meta<typeof RolloutColumnState>;

export default meta;

type Story = StoryObj<typeof RolloutColumnState>;

interface Row {
  miner: string;
  phase: RolloutTargetPhase;
  doneLabel?: string;
  idleLabel?: string;
}

const rows: Row[] = [
  { miner: "M-1042", phase: "done", doneLabel: "5.1.0" },
  { miner: "M-1043", phase: "inProgress" },
  { miner: "M-1044", phase: "retrying" },
  { miner: "M-1045", phase: "failed" },
  { miner: "M-1046", phase: "queued", idleLabel: "5.0.2" },
  { miner: "M-1047", phase: "excluded" },
];

/** All phase states as they read inside the fleet table's Firmware column,
 * including the auto-retry (`Retrying`) state. */
export const AllStates: Story = {
  name: "All firmware-column states",
  render: () => (
    <div className="w-[420px] max-w-full overflow-hidden rounded-xl border border-border-5 bg-surface-elevated-base">
      <div className="grid grid-cols-[1fr_auto] gap-4 border-b border-border-5 px-4 py-2.5 text-200 text-text-primary-50">
        <span>Miner</span>
        <span>Firmware</span>
      </div>
      {rows.map((row) => (
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
