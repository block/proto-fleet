import PoolSelectionPageComponent from "./PoolSelectionPage";

interface PoolSelectionPageArgs {
  numberOfMiners: number;
  numberOfPools: number;
}

export const PoolSelectionPage = ({ numberOfMiners, numberOfPools }: PoolSelectionPageArgs) => {
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
    {
      poolId: "3",
      name: "Client pool A3",
      poolUrl: "stratum+tcp://mine.ocean.xyz:3344",
      username: "mann27",
    },
  ];

  const deviceIdentifiers = Array.from({ length: numberOfMiners }, (_, i) => `device-${i}`);

  return (
    <PoolSelectionPageComponent
      deviceIdentifiers={deviceIdentifiers}
      availablePools={availablePools.splice(0, numberOfPools)}
      onAssignPools={async () => {}}
      onDismiss={() => {}}
    />
  );
};

export default {
  title: "Proto Fleet/Action Bar/Settings widget/Pool selection page",
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
