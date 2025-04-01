import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Rectangle = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)}>
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 12 12"
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        preserveAspectRatio="xMidYMid meet"
      >
        <rect
          width="8"
          height="10"
          x="1"
          y="1"
          stroke="currentColor"
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeOpacity=".5"
          strokeWidth="1.5"
          rx="1.5"
        />
      </svg>
    </div>
  );
};

export default Rectangle;
