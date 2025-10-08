import { useEffect } from "react";
import HashrateComponent from "./Hashrate";
import { Measurement } from "@/protoFleet/api/generated/common/v1/measurement_pb";
import { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useFleetStore } from "@/protoFleet/features/fleetManagement/store/useFleetStore";

const mockMiner = {
  deviceIdentifier: "story-miner-1",
  name: "Story Miner",
  hashrate: [
    {
      timestamp: { seconds: BigInt(1641024000), nanos: 0 },
      value: 189,
    } as Measurement,
    {
      timestamp: { seconds: BigInt(1641110400), nanos: 0 },
      value: 194,
    } as Measurement,
    {
      timestamp: { seconds: BigInt(1641196800), nanos: 0 },
      value: 190,
    } as Measurement,
    {
      timestamp: { seconds: BigInt(1641283200), nanos: 0 },
      value: 213.2,
    } as Measurement,
  ],
};

export const WithDeviceIdentifier = () => {
  const { setMiners } = useFleetStore();

  useEffect(() => {
    setMiners([mockMiner as MinerStateSnapshot]);
  }, [setMiners]);

  return (
    <div className="w-40">
      <HashrateComponent deviceIdentifier="story-miner-1" />
    </div>
  );
};

export const WithDirectProps = () => {
  return (
    <div className="w-40">
      <HashrateComponent hashrate={mockMiner.hashrate} />
    </div>
  );
};

export default {
  title: "Proto Fleet/MinerList/Hashrate",
};
