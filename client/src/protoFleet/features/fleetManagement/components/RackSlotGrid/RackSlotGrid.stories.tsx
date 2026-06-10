import { useMemo } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import RackSlotGrid from "./RackSlotGrid";
import type { NumberingOrigin, SlotVisualState } from "./types";

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
function InteractiveRackSlotGrid({
  rows,
  cols,
  occupiedCount,
  selectedCount,
  dragOverCount,
  peerHoverCount,
  ...rest
}: {
  rows: number;
  cols: number;
  occupiedCount: number;
  selectedCount: number;
  dragOverCount: number;
  peerHoverCount: number;
  numberingOrigin?: NumberingOrigin;
  slotSize?: number;
}) {
  const slotStates = useMemo(() => {
    const total = rows * cols;
    const indices = Array.from({ length: total }, (_, i) => i);
    const shuffled = seededShuffle(indices, 42);

    const states: SlotVisualState[] = [];
    const counts: [SlotVisualState, number][] = [
      ["occupied", occupiedCount],
      ["selected", selectedCount],
      ["dragOver", dragOverCount],
      ["peerHover", peerHoverCount],
    ];
    for (const [state, qty] of counts) {
      for (let i = 0; i < Math.min(qty, total - states.length); i++) states.push(state);
    }
    while (states.length < total) states.push("empty");

    const map: Record<string, SlotVisualState> = {};
    for (let i = 0; i < total; i++) {
      const idx = shuffled[i];
      const row = Math.floor(idx / cols);
      const col = idx % cols;
      map[`${row}-${col}`] = states[i];
    }
    return map;
  }, [rows, cols, occupiedCount, selectedCount, dragOverCount, peerHoverCount]);

  return <RackSlotGrid rows={rows} cols={cols} slotStates={slotStates} {...rest} />;
}

const meta: Meta<typeof InteractiveRackSlotGrid> = {
  title: "Proto Fleet/Rack Management/RackSlotGrid",
  component: InteractiveRackSlotGrid,
  parameters: {
    layout: "centered",
  },
  tags: ["autodocs"],
  argTypes: {
    rows: { control: { type: "range", min: 2, max: 12, step: 1 } },
    cols: { control: { type: "range", min: 2, max: 12, step: 1 } },
    slotSize: { control: { type: "range", min: 24, max: 64, step: 1 } },
    numberingOrigin: {
      control: "select",
      options: ["bottom-left", "top-left", "bottom-right", "top-right"],
    },
    occupiedCount: { control: { type: "number", min: 0, max: 144, step: 1 } },
    selectedCount: { control: { type: "number", min: 0, max: 144, step: 1 } },
    dragOverCount: { control: { type: "number", min: 0, max: 144, step: 1 } },
    peerHoverCount: { control: { type: "number", min: 0, max: 144, step: 1 } },
  },
};

export default meta;
type Story = StoryObj<typeof InteractiveRackSlotGrid>;

export const Default: Story = {
  args: {
    rows: 5,
    cols: 5,
    occupiedCount: 15,
    selectedCount: 0,
    dragOverCount: 0,
    peerHoverCount: 0,
  },
};

export const AllSlotStates: Story = {
  render: () => (
    <RackSlotGrid
      rows={2}
      cols={3}
      slotStates={{
        "0-0": "empty",
        "0-1": "occupied",
        "0-2": "selected",
        "1-0": "selectedOccupied",
        "1-1": "dragOver",
        "1-2": "peerHover",
      }}
    />
  ),
};

export const Empty: Story = {
  args: {
    rows: 5,
    cols: 5,
    occupiedCount: 0,
    selectedCount: 0,
    dragOverCount: 0,
    peerHoverCount: 0,
  },
};

export const PickMode: Story = {
  args: {
    rows: 5,
    cols: 5,
    occupiedCount: 12,
    selectedCount: 0,
    dragOverCount: 0,
    peerHoverCount: 0,
  },
};

export const DragOverState: Story = {
  args: {
    rows: 5,
    cols: 5,
    occupiedCount: 12,
    selectedCount: 0,
    dragOverCount: 1,
    peerHoverCount: 0,
  },
};

export const PeerHoverState: Story = {
  args: {
    rows: 5,
    cols: 5,
    occupiedCount: 12,
    selectedCount: 0,
    dragOverCount: 0,
    peerHoverCount: 1,
  },
};

export const WithSelection: Story = {
  args: {
    rows: 5,
    cols: 5,
    occupiedCount: 15,
    selectedCount: 1,
    dragOverCount: 0,
    peerHoverCount: 0,
  },
};

export const Complete: Story = {
  args: {
    rows: 5,
    cols: 5,
    occupiedCount: 25,
    selectedCount: 0,
    dragOverCount: 0,
    peerHoverCount: 0,
  },
};

export const CompactRack: Story = {
  args: {
    rows: 8,
    cols: 4,
    occupiedCount: 20,
    selectedCount: 0,
    dragOverCount: 0,
    peerHoverCount: 0,
  },
};

export const WideRack: Story = {
  args: {
    rows: 6,
    cols: 8,
    occupiedCount: 30,
    selectedCount: 0,
    dragOverCount: 0,
    peerHoverCount: 0,
  },
};

export const TopLeftNumbering: Story = {
  args: {
    rows: 5,
    cols: 5,
    numberingOrigin: "top-left",
    occupiedCount: 0,
    selectedCount: 0,
    dragOverCount: 0,
    peerHoverCount: 0,
  },
};

export const TopRightNumbering: Story = {
  args: {
    rows: 5,
    cols: 5,
    numberingOrigin: "top-right",
    occupiedCount: 0,
    selectedCount: 0,
    dragOverCount: 0,
    peerHoverCount: 0,
  },
};

export const BottomLeftNumbering: Story = {
  args: {
    rows: 5,
    cols: 5,
    numberingOrigin: "bottom-left",
    occupiedCount: 0,
    selectedCount: 0,
    dragOverCount: 0,
    peerHoverCount: 0,
  },
};

export const BottomRightNumbering: Story = {
  args: {
    rows: 5,
    cols: 5,
    numberingOrigin: "bottom-right",
    occupiedCount: 0,
    selectedCount: 0,
    dragOverCount: 0,
    peerHoverCount: 0,
  },
};

export const MaxSize: Story = {
  args: {
    rows: 12,
    cols: 12,
    slotSize: 28,
    occupiedCount: 42,
    selectedCount: 0,
    dragOverCount: 0,
    peerHoverCount: 0,
  },
};
