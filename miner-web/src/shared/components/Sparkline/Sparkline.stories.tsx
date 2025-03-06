import SparklineComponent from ".";

const data = [
  { time: 1641024000000, y: 5 },
  { time: 1641110400000, y: 14 },
  { time: 1641196800000, y: 190 },
  { time: 1641283200000, y: 2 },
];

export const Sparkline = () => {
  return (
    <div className="w-40 h-8">
      <SparklineComponent data={data} />
    </div>
  );
};

export default {
  title: "Components (Shared)/Sparkline",
};
