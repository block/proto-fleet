import clsx from "clsx";

import { StatusCircleProps } from "./types";
import { Circle, ConcentricCircles } from "@/shared/assets/icons";

const statusColors = {
  normal: "text-intent-success-fill",
  error: "text-intent-critical-fill",
  warning: "text-intent-warning-fill",
  inactive: "text-grayscale-gray-50",
  pending: "text-intent-info-fill",
  sleeping: "text-core-primary-20",
};

const StatusCircle = ({
  status,
  width,
  variant = "primary",
  removeMargin = false,
  isSelected = false,
}: StatusCircleProps) => {
  const colorClass = isSelected
    ? "text-intent-info-fill"
    : statusColors[status];

  return (
    <>
      {variant == "simple" ? (
        <Circle
          className={clsx(colorClass, { "mr-1": !removeMargin })}
          width={width}
        />
      ) : (
        <ConcentricCircles
          className={clsx(colorClass, { "mr-1": !removeMargin })}
          width={width}
        />
      )}
    </>
  );
};

export default StatusCircle;
