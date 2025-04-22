import PoolsModalComponent from "./PoolsModal";

interface PoolsModalArgs {
  numberOfMiners: number;
  numberOfPools: number;
}

export const PoolsModal = ({
  numberOfMiners,
  numberOfPools,
}: PoolsModalArgs) => {
  const availablePools = [
    {
      poolUrl: "stratum+tcp://mine.ocean.xyz:3334",
      username: "mann23",
    },
    {
      poolUrl: "stratum+tcp://mine.ocean.xyz:3323",
      username: "mann25",
    },
    {
      poolUrl: "stratum+tcp://mine.ocean.xyz:3344",
      username: "mann27",
    },
  ];

  return (
    <PoolsModalComponent
      numberOfMiners={numberOfMiners}
      availablePools={availablePools.splice(0, numberOfPools)}
      onDismiss={() => {}}
    />
  );
};

export default {
  title: "Components (ProtoFleet)/Action Bar/Settings widget/Pools modal",
  args: {
    numberOfMiners: 1,
    numberOfPools: 1,
  },
  argTypes: {
    numberOfMiners: {
      control: { type: "range", min: 1, max: 25, step: 1 },
    },
    numberOfPools: {
      control: { type: "range", min: 1, max: 3, step: 1 },
    },
  },
};
