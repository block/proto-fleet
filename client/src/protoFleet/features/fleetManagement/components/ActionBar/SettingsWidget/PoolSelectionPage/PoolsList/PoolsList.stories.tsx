import type { ReactNode } from "react";
import PoolsListComponent from ".";
import { MockedPoolApis } from "@/protoFleet/stories/MockedPoolApis";

const withMockedPoolApis = (Story: () => ReactNode) => (
  <MockedPoolApis>
    <Story />
  </MockedPoolApis>
);

interface PoolsListArgs {
  title: string;
  subtitle: string;
  createNewLabel: string;
  poolNumber?: number;
}

export const PoolsList = ({ title, subtitle, createNewLabel, poolNumber }: PoolsListArgs) => {
  return (
    <PoolsListComponent
      title={title}
      subtitle={subtitle}
      onSelect={() => {}}
      createNewLabel={createNewLabel}
      poolNumber={poolNumber}
    />
  );
};

export default {
  title: "Proto Fleet/Action Bar/Settings widget/Pools modal/Pools list",
  decorators: [withMockedPoolApis],
  args: {
    title: "Default pool",
    subtitle: "",
    createNewLabel: "Add pool",
    poolNumber: undefined,
  },
  argTypes: {
    title: {
      control: "text",
    },
    subtitle: {
      control: "text",
    },
    createNewLabel: {
      control: "text",
    },
    poolNumber: {
      control: "number",
    },
  },
};
