import { useMemo } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import RackDetailGrid from "./RackDetailGrid";
import type { NumberingOrigin, SlotHealthState } from "./types";

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

/** Interactive wrapper that derives slotStates from count-based controls */
function InteractiveRackDetailGrid({
  rows,
  cols,
  healthyCount,
  needsAttentionCount,
  offlineCount,
  sleepingCount,
  ...rest
}: {
  rows: number;
  cols: number;
  healthyCount: number;
  needsAttentionCount: number;
  offlineCount: number;
  sleepingCount: number;
  numberingOrigin?: NumberingOrigin;
  slotSize?: number;
}) {
  const slotStates = useMemo(() => {
    const total = rows * cols;
    const indices = Array.from({ length: total }, (_, i) => i);
    const shuffled = seededShuffle(indices, 42);

    const states: SlotHealthState[] = [];
    const counts: [SlotHealthState, number][] = [
      ["healthy", healthyCount],
      ["needsAttention", needsAttentionCount],
      ["offline", offlineCount],
      ["sleeping", sleepingCount],
    ];
    for (const [state, qty] of counts) {
      for (let i = 0; i < Math.min(qty, total - states.length); i++) states.push(state);
    }
    while (states.length < total) states.push("empty");

    const map: Record<string, SlotHealthState> = {};
    for (let i = 0; i < total; i++) {
      const idx = shuffled[i];
      const row = Math.floor(idx / cols);
      const col = idx % cols;
      map[`${row}-${col}`] = states[i];
    }
    return map;
  }, [rows, cols, healthyCount, needsAttentionCount, offlineCount, sleepingCount]);

  return <RackDetailGrid rows={rows} cols={cols} slotStates={slotStates} {...rest} />;
}

const meta: Meta<typeof InteractiveRackDetailGrid> = {
  title: "Proto Fleet/Rack Management/RackDetailGrid",
  component: InteractiveRackDetailGrid,
  parameters: {
    layout: "padded",
  },
  tags: ["autodocs"],
  argTypes: {
    rows: { control: { type: "range", min: 2, max: 12, step: 1 } },
    cols: { control: { type: "range", min: 2, max: 12, step: 1 } },
    slotSize: { control: { type: "range", min: 32, max: 80, step: 1 } },
    numberingOrigin: {
      control: "select",
      options: ["bottom-left", "top-left", "bottom-right", "top-right"],
    },
    healthyCount: { control: { type: "number", min: 0, max: 144, step: 1 } },
    needsAttentionCount: { control: { type: "number", min: 0, max: 144, step: 1 } },
    offlineCount: { control: { type: "number", min: 0, max: 144, step: 1 } },
    sleepingCount: { control: { type: "number", min: 0, max: 144, step: 1 } },
  },
};

export default meta;
type Story = StoryObj<typeof InteractiveRackDetailGrid>;

export const Default: Story = {
  args: {
    rows: 5,
    cols: 5,
    healthyCount: 25,
    needsAttentionCount: 0,
    offlineCount: 0,
    sleepingCount: 0,
  },
};

export const AllSlotStates: Story = {
  render: () => (
    <RackDetailGrid
      rows={2}
      cols={3}
      slotStates={{
        "0-0": "healthy",
        "0-1": "needsAttention",
        "0-2": "offline",
        "1-0": "sleeping",
        "1-1": "empty",
        "1-2": "healthy",
      }}
    />
  ),
};

export const Empty: Story = {
  args: {
    rows: 5,
    cols: 5,
    healthyCount: 0,
    needsAttentionCount: 0,
    offlineCount: 0,
    sleepingCount: 0,
  },
};

export const WithIssues: Story = {
  args: {
    rows: 5,
    cols: 5,
    healthyCount: 15,
    needsAttentionCount: 5,
    offlineCount: 3,
    sleepingCount: 2,
  },
};

export const MostlyHealthy: Story = {
  args: {
    rows: 5,
    cols: 5,
    healthyCount: 22,
    needsAttentionCount: 2,
    offlineCount: 1,
    sleepingCount: 0,
  },
};

export const AllSleeping: Story = {
  args: {
    rows: 5,
    cols: 5,
    healthyCount: 0,
    needsAttentionCount: 0,
    offlineCount: 0,
    sleepingCount: 25,
  },
};

export const CompactRack: Story = {
  args: {
    rows: 8,
    cols: 4,
    healthyCount: 26,
    needsAttentionCount: 2,
    offlineCount: 2,
    sleepingCount: 0,
  },
};

export const WideRack: Story = {
  args: {
    rows: 6,
    cols: 8,
    healthyCount: 36,
    needsAttentionCount: 4,
    offlineCount: 2,
    sleepingCount: 2,
  },
};

export const TopLeftNumbering: Story = {
  args: {
    rows: 5,
    cols: 5,
    numberingOrigin: "top-left",
    healthyCount: 25,
    needsAttentionCount: 0,
    offlineCount: 0,
    sleepingCount: 0,
  },
};

export const BottomRightNumbering: Story = {
  args: {
    rows: 5,
    cols: 5,
    numberingOrigin: "bottom-right",
    healthyCount: 25,
    needsAttentionCount: 0,
    offlineCount: 0,
    sleepingCount: 0,
  },
};

export const MaxSize: Story = {
  args: {
    rows: 12,
    cols: 12,
    slotSize: 40,
    healthyCount: 100,
    needsAttentionCount: 20,
    offlineCount: 10,
    sleepingCount: 6,
  },
};
