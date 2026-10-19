import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import PoolStatisticsView from "./PoolStatisticsView";
import type { PoolDuration } from "./usePoolStatistics";
import { asPoolId } from "@/protoOS/store/contract";

const pools = ["Default pool", "Backup 1", "Backup 2"].map((name, index) => ({
  id: asPoolId(`pool:${index + 1}`),
  name,
  priority: index,
  url: "mine.ocean.xyz:3334",
  username: `ct1.worker-${index + 1}`,
  status: index === 0 ? "Active" : "Idle",
  difficulty: 524_300,
}));
const meta = {
  title: "Proto OS/Pools/Statistics",
  component: PoolStatisticsView,
  parameters: { layout: "fullscreen" },
  args: {
    pools,
    duration: "24h",
    configureHref: "/settings/mining-pools",
    onDurationChange: () => {},
    history: {
      byPool: Object.fromEntries(
        pools.map((pool) => [
          pool.id,
          { accepted: 71_900, rejected: 305, acceptanceRate: 71_900 / 72_205, partial: false },
        ]),
      ),
      unavailable: [],
    },
  },
  render: function Render(args) {
    const [duration, setDuration] = useState<PoolDuration>("24h");
    return <PoolStatisticsView {...args} duration={duration} onDurationChange={setDuration} />;
  },
} satisfies Meta<typeof PoolStatisticsView>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Default: Story = {};
export const MissingHistory: Story = { args: { history: { byPool: {}, unavailable: pools.map((pool) => pool.id) } } };
export const Empty: Story = { args: { pools: [], history: undefined } };
export const Loading: Story = { args: { pools: undefined, history: undefined, pending: true } };
