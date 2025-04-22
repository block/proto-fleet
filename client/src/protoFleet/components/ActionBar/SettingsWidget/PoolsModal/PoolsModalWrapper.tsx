import PoolsModal from "./PoolsModal";

interface PoolsModalWrapperProps {
  numberOfMiners: number;
  onDismiss: (poolsChanged: boolean) => void;
}

const PoolsModalWrapper = ({
  numberOfMiners,
  onDismiss,
}: PoolsModalWrapperProps) => {
  // TODO fetch pools from API
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
    <PoolsModal
      numberOfMiners={numberOfMiners}
      availablePools={availablePools}
      onDismiss={onDismiss}
    />
  );
};

export default PoolsModalWrapper;
