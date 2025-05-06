import useFleet from "@/protoFleet/api/useFleet";
import MinerList from "@/protoFleet/features/fleetManagement/components/MinerList";

const Fleet = () => {
  const { miners } = useFleet();
  return (
    <>
      <MinerList title="Miners" miners={miners} />
    </>
  );
};

export default Fleet;
