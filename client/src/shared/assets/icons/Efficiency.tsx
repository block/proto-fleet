import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Efficiency = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="efficiency-icon">
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 20 20"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="xMidYMid meet"
      >
        <path
          stroke="#000"
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeOpacity=".9"
          strokeWidth="2"
          d="M18.663 6.85c.59-2.04.424-3.81-.64-4.874-2.249-2.249-7.664-.48-12.096 3.951A22.28 22.28 0 0 0 4.6 7.377m10.8 5.247c-.411.49-.854.975-1.327 1.449-4.432 4.431-9.847 6.2-12.097 3.95C.904 16.953.746 15.16 1.352 13.1M10.675 7.3l-1.75 2.07c-.215.278-.321.416-.255.523.066.107.259.107.644.107h1.373c.385 0 .578 0 .644.107.066.107-.04.245-.255.523L9.314 12.7m3.477-7.958c.434.37.862.766 1.282 1.185 4.431 4.432 6.2 9.847 3.95 12.097-1.106 1.106-2.978 1.24-5.122.564M6.582 1.264c-1.926-.5-3.59-.304-4.606.712-2.249 2.25-.48 7.665 3.951 12.097a22.3 22.3 0 0 0 1.332 1.229"
        />
      </svg>
    </div>
  );
};

export default Efficiency;
