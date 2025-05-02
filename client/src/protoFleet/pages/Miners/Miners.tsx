import useFleet from "@/protoFleet/api/useFleet";
import MinerList from "@/protoFleet/components/MinerList";

const Miners = () => {
  const { miners } = useFleet();
  return (
    <>
      <MinerList title="Miners" miners={miners} />
    </>
  );
};

export default Miners;
