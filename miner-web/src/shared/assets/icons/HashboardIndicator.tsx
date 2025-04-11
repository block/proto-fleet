import { Fragment } from "react";
import clsx from "clsx";

import { IconProps } from "./types";

type HashboardIndicatorProps = IconProps & {
  activeHashboard?: number;
  totalHashboards?: number;
};

const HashboardIndicator = ({
  className,
  activeHashboard = 0,
  totalHashboards = 9,
}: HashboardIndicatorProps) => {
  return (
    <div
      className={clsx(
        "inline-flex items-center justify-center gap-[3px] rounded-[4px] border-1 border-core-primary-10",
        {
          "px-[3px]": totalHashboards > 3,
          "p-[3px]": totalHashboards <= 3,
        },
        className,
      )}
    >
      {new Array(totalHashboards).fill(null).map((_, index) => {
        const renderDivider =
          index !== totalHashboards - 1 && (index + 1) % 3 === 0;

        return (
          <Fragment key={"hb-slot-" + index}>
            <div
              className={clsx("h-3 w-0.5 rounded-[1px]", {
                "bg-text-primary": activeHashboard === index,
                "bg-core-primary-20": activeHashboard !== index,
              })}
            />
            {renderDivider && (
              <div className="h-4.5 w-[1px] bg-core-primary-20" />
            )}
          </Fragment>
        );
      })}
    </div>
  );
};

export default HashboardIndicator;
