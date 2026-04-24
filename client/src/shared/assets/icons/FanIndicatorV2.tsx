import clsx from "clsx";

type FanIndicatorProps = {
  totalSlots?: number;
  position?: number;
};

const FanIndicatorV2 = ({ totalSlots: numFans = 6, position = 1 }: FanIndicatorProps) => {
  const numCols = Math.ceil(numFans / 2);

  return (
    <div className="flex px-[3px]">
      {Array(numCols)
        .fill(0)
        .map((_, colIdx) => {
          const firstFanIndex = colIdx * 2;
          const secondFanIndex = firstFanIndex + 1;
          const isFirstActive = position === firstFanIndex + 1;
          const isSecondActive = position === secondFanIndex + 1;
          const hasActiveFan = isFirstActive || isSecondActive;

          return (
            <div
              key={colIdx}
              className={clsx("flex flex-col items-center justify-center gap-0.5 p-0.5", {
                "rounded border-2 border-border-10": hasActiveFan,
              })}
            >
              <div
                className={clsx("h-1.5 w-1.5 rounded-full bg-core-primary-20", {
                  "bg-text-primary": isFirstActive,
                })}
              />
              {secondFanIndex < numFans ? (
                <div
                  className={clsx("h-1.5 w-1.5 rounded-full bg-core-primary-20", {
                    "bg-text-primary": isSecondActive,
                  })}
                />
              ) : null}
            </div>
          );
        })}
    </div>
  );
};

export default FanIndicatorV2;
