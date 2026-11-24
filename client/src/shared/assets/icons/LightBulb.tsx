import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const LightBulb = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="light-bulb-icon">
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 20 20"
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        preserveAspectRatio="xMidYMid meet"
      >
        <path
          d="M14.5 11.25A6 6 0 1 0 5.5 11.25c.83.73 1.5 1.49 1.5 2.75h6c0-1.26.67-2.02 1.5-2.75Z"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <path d="M7.5 17h5" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
      </svg>
    </div>
  );
};

export default LightBulb;
