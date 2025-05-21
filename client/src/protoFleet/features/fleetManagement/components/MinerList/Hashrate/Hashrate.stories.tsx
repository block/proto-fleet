import HashrateComponent from ".";
import { Measurement } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

const hashrate = [
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
];

export const Hashrate = () => {
  return (
    <div className="w-40">
      <HashrateComponent hashrate={hashrate} />
    </div>
  );
};

export default {
  title: "Components (ProtoFleet)/MinerList/Hashrate",
};
