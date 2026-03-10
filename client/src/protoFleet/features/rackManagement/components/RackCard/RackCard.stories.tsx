import { useMemo } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import RackCard from "./RackCard";
import RackCardGrid from "./RackCardGrid";
import type { RackStatus, SlotStatus } from "./types";

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
function makeSlots(
  total: number,
  healthy: number,
  needsAttention = 0,
  offline = 0,
  sleeping = 0,
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

/** Interactive wrapper that derives slots from miner count controls */
function InteractiveRackCard({
  cols,
  rows,
  healthyCount,
  needsAttentionCount,
  offlineCount,
  sleepingCount,
  ...rest
}: {
  label: string;
  building?: string;
  cols: number;
  rows: number;
  healthyCount: number;
  needsAttentionCount: number;
  offlineCount: number;
  sleepingCount: number;
  status: RackStatus;
  statusText: string;
  hashrate?: string;
  efficiency?: string;
  power?: string;
  temperature?: string;
  onClick?: () => void;
}) {
  const total = cols * rows;
  const slots = useMemo(
    () => makeSlots(total, healthyCount, needsAttentionCount, offlineCount, sleepingCount),
    [total, healthyCount, needsAttentionCount, offlineCount, sleepingCount],
  );

  return <RackCard cols={cols} rows={rows} slots={slots} {...rest} />;
}

const meta: Meta<typeof InteractiveRackCard> = {
  title: "Proto Fleet/Rack Management/RackCard",
  component: InteractiveRackCard,
  parameters: {
    layout: "centered",
    docs: {
      description: {
        component:
          "Rack card showing a mini grid visualization of slot health, status summary, and performance stats. Remaining slots beyond miner counts are empty and scattered randomly.",
      },
    },
  },
  tags: ["autodocs"],
  argTypes: {
    label: { control: "text", description: "Rack label, e.g. R-01" },
    building: { control: "text", description: "Building / location label" },
    cols: { control: { type: "range", min: 2, max: 12, step: 1 }, description: "Grid columns" },
    rows: { control: { type: "range", min: 2, max: 12, step: 1 }, description: "Grid rows" },
    healthyCount: {
      control: { type: "number", min: 0, max: 144, step: 1 },
      description: "Number of healthy miners",
    },
    needsAttentionCount: {
      control: { type: "number", min: 0, max: 144, step: 1 },
      description: "Number of miners needing attention",
    },
    offlineCount: {
      control: { type: "number", min: 0, max: 144, step: 1 },
      description: "Number of offline miners",
    },
    sleepingCount: {
      control: { type: "number", min: 0, max: 144, step: 1 },
      description: "Number of sleeping miners",
    },
    status: {
      control: "select",
      options: ["healthy", "needsAttention", "offline", "sleeping", "mixed", "empty"],
      description: "Rack-level health status",
    },
    statusText: { control: "text", description: "Status description text" },
    hashrate: { control: "text", description: "Hashrate display value" },
    efficiency: { control: "text", description: "Efficiency display value" },
    power: { control: "text", description: "Total power display value" },
    temperature: { control: "text", description: "Temperature range" },
    onClick: { action: "clicked" },
  },
  decorators: [
    (Story) => (
      <div className="w-[260px]">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof InteractiveRackCard>;

export const Default: Story = {
  args: {
    label: "R-01",
    building: "Austin — Building 1",
    cols: 5,
    rows: 5,
    healthyCount: 25,
    needsAttentionCount: 0,
    offlineCount: 0,
    sleepingCount: 0,
    status: "healthy",
    statusText: "Healthy",
    hashrate: "1.2 PH/s",
    efficiency: "21.50 J/TH",
    power: "25.6 kW",
    temperature: "62°–71°",
  },
};

export const WithIssues: Story = {
  args: {
    label: "R-02",
    building: "Austin — Building 1",
    cols: 5,
    rows: 5,
    healthyCount: 17,
    needsAttentionCount: 5,
    offlineCount: 0,
    sleepingCount: 0,
    status: "needsAttention",
    statusText: "5 need attention",
    hashrate: "0.9 PH/s",
    efficiency: "23.10 J/TH",
    power: "20.8 kW",
    temperature: "64°–75°",
  },
};

export const WithOffline: Story = {
  args: {
    label: "R-03",
    building: "Austin — Building 2",
    cols: 5,
    rows: 5,
    healthyCount: 15,
    needsAttentionCount: 0,
    offlineCount: 7,
    sleepingCount: 0,
    status: "offline",
    statusText: "7 offline",
    hashrate: "0.7 PH/s",
    efficiency: "22.00 J/TH",
    power: "16.1 kW",
    temperature: "58°–68°",
  },
};

export const Sleeping: Story = {
  args: {
    label: "R-04",
    building: "Austin — Building 2",
    cols: 5,
    rows: 5,
    healthyCount: 12,
    needsAttentionCount: 0,
    offlineCount: 0,
    sleepingCount: 10,
    status: "sleeping",
    statusText: "10 sleeping",
    hashrate: "0.6 PH/s",
    efficiency: "20.80 J/TH",
    power: "12.0 kW",
    temperature: "50°–60°",
  },
};

export const Mixed: Story = {
  args: {
    label: "R-05",
    building: "Austin — Building 3",
    cols: 5,
    rows: 5,
    healthyCount: 12,
    needsAttentionCount: 4,
    offlineCount: 3,
    sleepingCount: 3,
    status: "mixed",
    statusText: "4 need attention · 3 offline",
    hashrate: "0.8 PH/s",
    efficiency: "22.70 J/TH",
    power: "18.3 kW",
    temperature: "59°–73°",
  },
};

export const Empty: Story = {
  args: {
    label: "R-06",
    building: "Austin — Building 3",
    cols: 5,
    rows: 5,
    healthyCount: 0,
    needsAttentionCount: 0,
    offlineCount: 0,
    sleepingCount: 0,
    status: "empty",
    statusText: "",
    hashrate: undefined,
    efficiency: undefined,
    power: undefined,
    temperature: undefined,
  },
};

export const SparseRack: Story = {
  args: {
    label: "R-07",
    building: "Austin — Building 2",
    cols: 6,
    rows: 6,
    healthyCount: 12,
    needsAttentionCount: 3,
    offlineCount: 2,
    sleepingCount: 1,
    status: "mixed",
    statusText: "3 need attention · 2 offline · 18 empty",
    hashrate: "0.5 PH/s",
    efficiency: "22.40 J/TH",
    power: "11.2 kW",
    temperature: "55°–64°",
  },
};

export const CompactRack: Story = {
  args: {
    label: "R-08",
    building: "Austin — Building 1",
    cols: 4,
    rows: 8,
    healthyCount: 26,
    needsAttentionCount: 2,
    offlineCount: 2,
    sleepingCount: 0,
    status: "mixed",
    statusText: "2 need attention · 2 offline",
    hashrate: "1.5 PH/s",
    efficiency: "21.00 J/TH",
    power: "31.5 kW",
    temperature: "60°–70°",
  },
};

export const WideRack: Story = {
  args: {
    label: "R-09",
    cols: 8,
    rows: 6,
    healthyCount: 36,
    needsAttentionCount: 4,
    offlineCount: 2,
    sleepingCount: 2,
    status: "mixed",
    statusText: "4 need attention · 2 offline",
    hashrate: "2.3 PH/s",
    efficiency: "21.80 J/TH",
    power: "50.1 kW",
    temperature: "61°–72°",
  },
};

export const MaxSize: Story = {
  args: {
    label: "R-10",
    building: "Austin — Building 4",
    cols: 12,
    rows: 12,
    healthyCount: 110,
    needsAttentionCount: 10,
    offlineCount: 8,
    sleepingCount: 6,
    status: "mixed",
    statusText: "10 need attention · 8 offline",
    hashrate: "6.9 PH/s",
    efficiency: "21.20 J/TH",
    power: "146.3 kW",
    temperature: "58°–74°",
  },
};

export const GridView: Story = {
  decorators: [
    (Story) => (
      <div className="w-[960px]">
        <Story />
      </div>
    ),
  ],
  render: () => (
    <RackCardGrid>
      <RackCard
        label="R-01"
        building="Building 1"
        cols={5}
        rows={5}
        slots={makeSlots(25, 25)}
        status="healthy"
        statusText="Healthy"
        hashrate="1.2 PH/s"
        efficiency="21.50 J/TH"
        power="25.6 kW"
        temperature="62°–71°"
      />
      <RackCard
        label="R-02"
        building="Building 1"
        cols={5}
        rows={5}
        slots={makeSlots(25, 17, 5, 0, 0, 99)}
        status="needsAttention"
        statusText="5 need attention"
        hashrate="0.9 PH/s"
        efficiency="23.10 J/TH"
        power="20.8 kW"
        temperature="64°–75°"
      />
      <RackCard
        label="R-03"
        building="Building 2"
        cols={6}
        rows={6}
        slots={makeSlots(36, 12, 3, 2, 1, 77)}
        status="mixed"
        statusText="3 need attention · 18 empty"
        hashrate="0.5 PH/s"
        efficiency="22.40 J/TH"
        power="11.2 kW"
        temperature="55°–64°"
      />
      <RackCard
        label="R-04"
        building="Building 2"
        cols={12}
        rows={12}
        slots={makeSlots(144, 110, 10, 8, 6, 55)}
        status="mixed"
        statusText="10 need attention · 8 offline"
        hashrate="6.9 PH/s"
        efficiency="21.20 J/TH"
        power="146.3 kW"
        temperature="58°–74°"
      />
      <RackCard
        label="R-05"
        building="Building 3"
        cols={5}
        rows={5}
        slots={makeSlots(25, 0)}
        status="empty"
        statusText=""
      />
      <RackCard
        label="R-06"
        building="Building 3"
        cols={6}
        rows={6}
        slots={makeSlots(36, 28, 0, 0, 6, 33)}
        status="sleeping"
        statusText="6 sleeping"
        hashrate="1.4 PH/s"
        efficiency="20.90 J/TH"
        power="29.3 kW"
        temperature="55°–65°"
      />
    </RackCardGrid>
  ),
};
