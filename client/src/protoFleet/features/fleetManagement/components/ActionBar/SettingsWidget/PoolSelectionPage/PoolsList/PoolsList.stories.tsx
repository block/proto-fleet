import PoolsListComponent from ".";

interface PoolsListArgs {
  title: string;
  subtitle: string;
  createNewLabel: string;
  poolNumber?: number;
}

export const PoolsList = ({
  title,
  subtitle,
  createNewLabel,
  poolNumber,
}: PoolsListArgs) => {
  const availablePools = [
    {
      poolId: "1",
      name: "Client pool A1",
      poolUrl: "stratum+tcp://mine.ocean.xyz:3334",
      username: "mann23",
    },
    {
      poolId: "2",
      name: "Client pool A2",
      poolUrl: "stratum+tcp://mine.ocean.xyz:3323",
      username: "mann25",
    },
  ];

  return (
    <PoolsListComponent
      title={title}
      subtitle={subtitle}
      availablePools={availablePools}
      onSelect={() => {}}
      createNewLabel={createNewLabel}
      poolNumber={poolNumber}
    />
  );
};

export default {
  title: "Proto Fleet/Action Bar/Settings widget/Pools modal/Pools list",
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
