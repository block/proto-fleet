import { useMemo, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import TankCard from "./TankCard";
import type { TankModuleState } from "./TankModuleGrid";

/** Deterministic shuffle so the module layout is stable across renders. */
function seededShuffle<T>(arr: T[], seed: number): T[] {
  const result = [...arr];
  let s = seed;
  for (let i = result.length - 1; i > 0; i--) {
    s = (s * 16807) % 2147483647;
    const j = s % (i + 1);
    [result[i], result[j]] = [result[j], result[i]];
  }
  return result;
}

/** Build a shuffled module array with `attention` needing-attention bars. */
function makeModules(total: number, attention = 0, seed = 7): TankModuleState[] {
  const modules: TankModuleState[] = [];
  for (let i = 0; i < Math.min(attention, total); i++) modules.push("attention");
  while (modules.length < total) modules.push("healthy");
  return seededShuffle(modules, seed);
}

const TANK_COLS = 8;
const TANK_ROWS = 2;
const MODULES = TANK_COLS * TANK_ROWS;

interface WrapperArgs {
  label: string;
  attention: number;
  startOn: boolean;
  stats: string[];
  seed: number;
}

/** Interactive wrapper so the power toggle flips live in the story. */
function InteractiveTankCard({ label, attention, startOn, stats, seed }: WrapperArgs) {
  const [on, setOn] = useState(startOn);
  const modules = useMemo(() => makeModules(MODULES, attention, seed), [attention, seed]);
  return (
    <TankCard
      label={label}
      cols={TANK_COLS}
      rows={TANK_ROWS}
      modules={modules}
      on={on}
      onToggle={setOn}
      onInfo={() => {}}
      stats={stats}
      onModuleAction={() => {}}
    />
  );
}

const meta: Meta<typeof InteractiveTankCard> = {
  title: "Proto Fleet/Containers/TankCard",
  component: InteractiveTankCard,
  parameters: {
    layout: "centered",
    docs: {
      description: {
        component:
          "A single tank on the container overview. Its body is the distinct TankModuleGrid visual (tall, spaced module bars — grey when healthy, two-tone orange when a module needs attention), the tank analogue of rack-detail's RackHealthModule rather than the dense fleet MiniRackGrid. Header pairs the power toggle with a circular info button; the footer spreads the readouts across the card width.",
      },
    },
  },
  decorators: [
    (Story) => (
      <div className="w-[360px]">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof InteractiveTankCard>;

export const Healthy: Story = {
  args: {
    label: "Tank 1",
    attention: 0,
    startOn: true,
    stats: ["48/48 boards", "65.5°", "12.3 kW"],
    seed: 11,
  },
};

export const WithIssues: Story = {
  args: {
    label: "Tank 2",
    attention: 3,
    startOn: true,
    stats: ["45/48 boards", "67.1°", "11.8 kW"],
    seed: 23,
  },
};

export const Off: Story = {
  args: {
    label: "Tank 6",
    attention: 0,
    startOn: false,
    stats: ["0/48 boards", "—", "0.0 kW"],
    seed: 67,
  },
};
