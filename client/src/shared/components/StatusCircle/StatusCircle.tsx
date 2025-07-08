import clsx from "clsx";

import { StatusCircleProps } from "./types";
import { ConcentricCircles } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";

// This has to be duplicated because the class names have to be complete
// Otherwise tailwind might not bundle them due to tree shaking and the colors won't work
const bgStatusColors = {
  normal: "bg-intent-success-fill",
  error: "bg-intent-critical-fill",
  warning: "bg-intent-warning-fill",
  inactive: "bg-grayscale-gray-50",
  pending: "bg-intent-info-fill",
  sleeping: "bg-core-primary-20",
};

const textStatusColors = {
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
  const bgColorClass = isSelected
    ? "bg-intent-info-fill"
    : bgStatusColors[status];
  const textColorClass = isSelected
    ? "text-intent-info-fill"
    : textStatusColors[status];

  return (
    <>
      {variant == "simple" ? (
        <div
          className={clsx(
            "aspect-square rounded-[50%]",
            !removeMargin && "mr-1",
            bgColorClass,
            width || iconSizes.xSmall,
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
