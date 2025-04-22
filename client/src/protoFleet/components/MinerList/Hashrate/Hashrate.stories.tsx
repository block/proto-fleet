import HashrateComponent from ".";

const hashrate = [
  { time: 1641024000000, hashrate: 189 },
  { time: 1641110400000, hashrate: 194 },
  { time: 1641196800000, hashrate: 190 },
  { time: 1641283200000, hashrate: 213.2 },
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
