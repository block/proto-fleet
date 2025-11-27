import clsx from "clsx";

import { IconProps } from "./types";

type HashboardIndicatorV2Props = IconProps & {
  color?: string;
  // slots are indexed from 1 to totalSlots, however some slots might be empty
  position?: number;
  totalSlots?: number;
};

const HashboardIndicatorV2 = ({ className, color, position = 1, totalSlots = 9 }: HashboardIndicatorV2Props) => {
  // Determine which chunk (group of 3) contains the active position
  const activeChunk = Math.ceil(position / 3);
  const numChunks = Math.ceil(totalSlots / 3);

  return (
    <div className={clsx("flex items-center justify-center px-[1px]", className)}>
      {Array.from({ length: numChunks }).map((_, chunkIndex) => {
        const chunkNumber = chunkIndex + 1;
        const isActiveChunk = chunkNumber === activeChunk;
        const startIdx = chunkIndex * 3;
        const barsInChunk = Math.min(3, totalSlots - startIdx);

        return (
          <div
            key={`chunk-${chunkIndex}`}
            className={clsx("flex h-6 items-center gap-0.5 p-0.5", {
              "rounded border-2 border-border-10": isActiveChunk,
              "py-[3px]": !isActiveChunk,
            })}
          >
            {Array.from({ length: barsInChunk }).map((_, barIndex) => {
              const currentPosition = startIdx + barIndex + 1;
              const isActive = position === currentPosition;

              return (
                <div
                  key={`bar-${currentPosition}`}
                  className={clsx("w-0.5 self-stretch rounded-[2px]", {
                    "bg-text-primary": isActive && !color,
                    "bg-core-primary-20": !isActive,
                  })}
                  style={{
                    backgroundColor: color && isActive ? `var(${color})` : undefined,
                  }}
                />
              );
            })}
          </div>
        );
      })}
    </div>
  );
};

export default HashboardIndicatorV2;
