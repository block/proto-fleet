import clsx from "clsx";

import { StatusCircleProps } from "./types";
import { ConcentricCircles } from "@/shared/assets/icons";

const statusColors = {
  normal: "intent-success-fill",
  error: "intent-critical-fill",
  warning: "intent-warning-fill",
  inactive: "grayscale-gray-50",
};

const StatusCircle = ({
  status,
  width,
  variant = "primary",
  removeMargin = false,
  isSelected = false,
}: StatusCircleProps) => {
  const bgColorClass = isSelected
    ? "bg-intent-info-fill"
    : `bg-${statusColors[status]}`;
  const textColorClass = isSelected
    ? "text-intent-info-fill"
    : `text-${statusColors[status]}`;

  return (
    <>
      {variant == "simple" ? (
        <div
          className={clsx(
            "aspect-square rounded-[50%]",
            !removeMargin && "mr-1",
            bgColorClass,
            width,
          )}
        />
      ) : (
        <ConcentricCircles
          className={clsx(!removeMargin && "mr-1", textColorClass)}
          width={width}
        />
      )}
    </>
  );
};

export default StatusCircle;
