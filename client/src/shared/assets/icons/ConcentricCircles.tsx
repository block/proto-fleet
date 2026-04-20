import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const ConcentricCircles = ({ className, width = iconSizes.xSmall }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="concentric-circles-icon">
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 12 12"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="xMidYMid meet"
      >
        <circle opacity=".4" cx="6" cy="6" r="5.5" stroke="currentColor" strokeOpacity=".8" />
        <circle cx="6" cy="6" r="4" fill="currentColor" fillOpacity="1" />
      </svg>
    </div>
  );
};

export default ConcentricCircles;
