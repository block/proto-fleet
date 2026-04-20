import clsx from "clsx";

type FanIndicatorProps = {
  numFans?: number;
  fanPosition?: number;
};

const FanIndicator = ({ numFans = 6, fanPosition = 1 }: FanIndicatorProps) => {
  const numCols = Math.ceil(numFans / 2);

  return (
    <div className="flex gap-1">
      {Array(numCols)
        .fill(0)
        .map((_, colIdx) => {
          const firstFanIndex = colIdx * 2;
          const secondFanIndex = firstFanIndex + 1;

          return (
            <div
              key={colIdx}
              className="flex w-[18px] flex-col items-center justify-start gap-0.5 rounded-sm border border-core-primary-20 p-0.5"
            >
              <div
                className={clsx("h-2 w-2 rounded-full bg-core-primary-20", {
                  "bg-text-primary": fanPosition === firstFanIndex + 1,
                })}
              />
              {secondFanIndex < numFans && (
                <div
                  className={clsx("h-2 w-2 rounded-full bg-core-primary-20", {
                    "bg-text-primary": fanPosition === secondFanIndex + 1,
                  })}
                />
              )}
            </div>
          );
        })}
    </div>
  );
};

export default FanIndicator;
