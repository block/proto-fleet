import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import ContainerPools, { type ContainerPoolsProps, type PoolDuration } from "./ContainerPools";
import type { ContainerPool } from "./PoolMonitorCard";

const pools: ContainerPool[] = [
  {
    id: "default",
    name: "Default pool",
    url: "mine.ocean.xyz:3334",
    role: "active",
    accepted: 71_900,
    rejected: 305,
    invalid: 17,
    difficulty: "524.3K",
    lastShare: "2s ago",
    bestShare: "165.6B",
    blocks: 1_190,
  },
  {
    id: "backup1",
    name: "Backup 1",
    url: "mine.ocean.xyz:3334",
    role: "standby",
    accepted: 71_900,
    rejected: 305,
    invalid: 17,
    difficulty: "524.3K",
    lastShare: "2s ago",
    bestShare: "165.6B",
    blocks: 1_190,
  },
  {
    id: "backup2",
    name: "Backup 2",
    url: "mine.ocean.xyz:3334",
    role: "standby",
    accepted: 0,
    rejected: 0,
    invalid: 0,
    difficulty: "—",
    lastShare: "—",
    bestShare: "—",
    blocks: 0,
  },
];

/** Interactive wrapper: owns the selected time range so the selector flips live. */
function InteractiveContainerPools() {
  const [duration, setDuration] = useState<PoolDuration>("24h");

  const props: ContainerPoolsProps = {
    pools,
    duration,
    onSelectDuration: setDuration,
  };

  return <ContainerPools {...props} />;
}

const meta: Meta<typeof InteractiveContainerPools> = {
  title: "Proto Fleet/Containers/Container Pools",
  component: InteractiveContainerPools,
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "Container Pools view (Frame 3): a read-only monitoring view of the pools the container is mining to, distinct from the Settings → Mining Pools config form. A 1h–5d time-range control sits above a stack of pools. The active pool gets prominence — its acceptance rate / accepted / rejected / invalid stats, an accepted/rejected/invalid split bar (black/red/grey with a legend), and its difficulty / last share / best share / blocks row are grouped in an elevated panel. Standby pools render compactly as two flat stat rows, separated by dividers. The 'Pools' nav label and 'Hashing' page-status chip belong to the host page chrome. Presentational and prop-driven; shared at container and module scope.",
      },
    },
  },
};

export default meta;
type Story = StoryObj<typeof InteractiveContainerPools>;

export const Default: Story = {};
