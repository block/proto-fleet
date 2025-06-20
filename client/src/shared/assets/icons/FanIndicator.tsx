import clsx from "clsx";

type FanIndicatorProps = {
  numFans: number;
  fanPosition?: number;
};

const FanIndicator = ({ numFans, fanPosition }: FanIndicatorProps) => {
  return (
    <div
      style={{ gridTemplateRows: "repeat(2, 1fr)" }}
      className="box-border grid grid-flow-col gap-0.5 rounded-sm border border-core-primary-20 p-1"
    >
      {Array(numFans)
        .fill(0)
        .map((_, index) => (
          <div
            key={index}
            className={clsx("h-1 w-1 rounded-full bg-core-primary-20", {
              "bg-core-primary-50": fanPosition === index,
            })}
          />
        ))}
    </div>
  );
};

export default FanIndicator;
