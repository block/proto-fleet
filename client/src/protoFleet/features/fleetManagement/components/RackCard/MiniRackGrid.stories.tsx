import { useMemo } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import MiniRackGrid from "./MiniRackGrid";
import type { SlotStatus } from "./types";

/** Deterministic shuffle using a seeded PRNG */
function seededShuffle<T>(arr: T[], seed: number): T[] {
  const result = [...arr];
  let s = seed;
  for (let i = result.length - 1; i > 0; i--) {
    s = (s * 16807 + 0) % 2147483647;
    const j = s % (i + 1);
    [result[i], result[j]] = [result[j], result[i]];
  }
  return result;
}

/** Build a shuffled slots array from miner counts, filling remaining with empty */
function buildSlots(
  total: number,
  healthy: number,
  needsAttention: number,
  offline: number,
  sleeping: number,
  seed = 42,
): SlotStatus[] {
  const slots: SlotStatus[] = [];
  const counts: [SlotStatus, number][] = [
    ["healthy", healthy],
    ["needsAttention", needsAttention],
    ["offline", offline],
    ["sleeping", sleeping],
  ];
  for (const [status, qty] of counts) {
    for (let i = 0; i < Math.min(qty, total - slots.length); i++) slots.push(status);
  }
  while (slots.length < total) slots.push("empty");
  return seededShuffle(slots, seed);
}

/** Interactive wrapper that rebuilds slots when count args change */
function InteractiveMiniRackGrid({
  cols,
  rows,
  healthy,
  needsAttention,
  offline,
  sleeping,
}: {
  cols: number;
  rows: number;
  healthy: number;
  needsAttention: number;
  offline: number;
  sleeping: number;
}) {
  const total = cols * rows;
  const slots = useMemo<SlotStatus[]>(
    () => buildSlots(total, healthy, needsAttention, offline, sleeping),
    [total, healthy, needsAttention, offline, sleeping],
  );

  return <MiniRackGrid cols={cols} rows={rows} slots={slots} />;
}

const meta: Meta<typeof InteractiveMiniRackGrid> = {
  title: "Proto Fleet/Rack Management/MiniRackGrid",
  component: InteractiveMiniRackGrid,
  parameters: {
    layout: "centered",
    docs: {
      description: {
        component:
          "Small grid of colored squares representing miner slot health within a rack. Empty slots are scattered randomly among occupied ones.",
      },
    },
  },
  tags: ["autodocs"],
  argTypes: {
    cols: {
      control: { type: "range", min: 2, max: 12, step: 1 },
      description: "Number of grid columns",
    },
    rows: {
      control: { type: "range", min: 2, max: 12, step: 1 },
      description: "Number of grid rows",
    },
    healthy: {
      control: { type: "number", min: 0, max: 144, step: 1 },
      description: "Number of healthy miners",
    },
    needsAttention: {
      control: { type: "number", min: 0, max: 144, step: 1 },
      description: "Number of miners needing attention",
    },
    offline: {
      control: { type: "number", min: 0, max: 144, step: 1 },
      description: "Number of offline miners",
    },
    sleeping: {
      control: { type: "number", min: 0, max: 144, step: 1 },
      description: "Number of sleeping miners",
    },
  },
  decorators: [
    (Story) => (
      <div className="rounded-2xl bg-surface-5 p-6">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof InteractiveMiniRackGrid>;

export const Small: Story = {
  args: { cols: 4, rows: 4, healthy: 12, needsAttention: 2, offline: 1, sleeping: 1 },
};

export const Medium: Story = {
  args: { cols: 6, rows: 6, healthy: 28, needsAttention: 4, offline: 2, sleeping: 2 },
};

export const Large: Story = {
  args: { cols: 10, rows: 10, healthy: 80, needsAttention: 10, offline: 5, sleeping: 5 },
};

export const Max: Story = {
  args: { cols: 12, rows: 12, healthy: 110, needsAttention: 15, offline: 10, sleeping: 9 },
};

export const SparseRack: Story = {
  args: { cols: 6, rows: 6, healthy: 10, needsAttention: 3, offline: 2, sleeping: 1 },
};

export const AllStatuses: Story = {
  args: { cols: 5, rows: 5, healthy: 10, needsAttention: 5, offline: 5, sleeping: 3 },
};
