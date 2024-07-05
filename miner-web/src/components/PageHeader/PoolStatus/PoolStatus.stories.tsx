import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";
import { action } from "@storybook/addon-actions";

import { Pool } from "apiTypes";

import PoolStatusComponent from "./PoolStatus";

interface PoolStatusProps {
  hasDefaultPool: boolean;
  loading: boolean;
  numberOfBackupPools: number;
  poolStatus: Pool["status"];
}

export const PoolStatus = ({
  hasDefaultPool,
  loading,
  numberOfBackupPools,
  poolStatus,
}: PoolStatusProps) => {
  return (
    <div className="w-96 flex justify-end">
      <PoolStatusComponent
        poolsInfo={
          [
            {
              ...(hasDefaultPool && {
                status: poolStatus,
                url: "mine.ocean.xyz:1111",
                priority: 0,
              }),
            },
            {
              ...(numberOfBackupPools >= 1 && {
                status: poolStatus,
                url: "mine.ocean.xyz:2222",
                priority: 1,
              }),
            },
            {
              ...(numberOfBackupPools === 2 && {
                status: poolStatus,
                url: "mine.ocean.xyz:3333",
                priority: 2,
              }),
            },
          ].filter((pool) => !!pool.url) as Pool[]
        }
        loading={loading}
        onClickViewPools={action("View mining pools clicked")}
        shouldShowPopover
      />
    </div>
  );
};

export default {
  title: "Components/Page Header/Pool Status",
  args: {
    hasDefaultPool: true,
    loading: false,
    numberOfBackupPools: 2,
    poolStatus: "Alive",
  },
  argTypes: {
    hasDefaultPool: {
      control: "boolean",
    },
    loading: {
      control: "boolean",
    },
    numberOfBackupPools: {
      control: "select",
      options: [0, 1, 2],
    },
    poolStatus: {
      control: "select",
      options: ["Active", "Alive", "Dead", "Disabled", "Rejecting"],
    },
  },
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
