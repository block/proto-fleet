import { Fragment } from "react";
import clsx from "clsx";

import { IconProps } from "./types";

type HashboardIndicatorProps = IconProps & {
  color?: string;
  // slots are indexed from 1 to totalHashboards, however some slots might be empty
  activeHashboardSlot?: number;
  totalHashboards?: number;
};

const HashboardIndicator = ({
  className,
  color,
  activeHashboardSlot = 1,
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
        const slotIndex = index + 1;
        const renderDivider = index !== totalHashboards - 1 && slotIndex % 3 === 0;

        return (
          <Fragment key={"hb-slot-" + index}>
            <div
              className={clsx("h-3 w-0.5 rounded-[1px]", {
                "bg-text-primary": activeHashboardSlot === slotIndex && !color,
                "bg-core-primary-20": activeHashboardSlot !== slotIndex,
              })}
              style={{
                backgroundColor: color && activeHashboardSlot === slotIndex ? `var(${color})` : undefined,
              }}
            />
            {renderDivider ? <div className="h-4.5 w-[1px] bg-core-primary-20" /> : null}
          </Fragment>
        );
      })}
    </div>
  );
};

export default HashboardIndicator;
