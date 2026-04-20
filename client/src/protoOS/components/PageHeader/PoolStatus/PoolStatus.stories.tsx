import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";
import { action } from "storybook/actions";

import PoolStatusComponent from "./PoolStatus";
import { Pool } from "@/protoOS/api/generatedApi";
import { PopoverProvider } from "@/shared/components/Popover";

interface PoolStatusProps {
  loading: boolean;
  numberOfPools: number;
  poolStatus: Pool["status"];
}

export const PoolStatus = ({ loading, numberOfPools, poolStatus }: PoolStatusProps) => {
  return (
    <div className="flex w-96 justify-end">
      <PopoverProvider>
        <PoolStatusComponent
          poolsInfo={
            loading
              ? undefined
              : ([
                  {
                    ...(numberOfPools >= 1 && {
                      status: poolStatus,
                      url: "stratum+tcp://stratum.braiins.com:999999999",
                      priority: 1,
                    }),
                  },
                  {
                    ...(numberOfPools >= 2 && {
                      status: poolStatus,
                      url: "mine.ocean.xyz:2222",
                      priority: 5,
                    }),
                  },
                  {
                    ...(numberOfPools === 3 && {
                      status: poolStatus,
                      url: "mine.ocean.xyz:3333",
                      priority: 8,
                    }),
                  },
                ].filter((pool) => !!pool.url) as Pool[])
          }
          loading={loading}
          onClickViewPools={action("View mining pools clicked")}
          shouldShowPopover
        />
      </PopoverProvider>
    </div>
  );
};

export default {
  title: "protoOS/Page Header/Pool Status",
  parameters: {
    withRouter: false,
  },
  args: {
    loading: false,
    numberOfPools: 3,
    poolStatus: "Active",
  },
  argTypes: {
    loading: {
      control: "boolean",
    },
    numberOfPools: {
      control: "select",
      options: [0, 1, 2, 3],
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
