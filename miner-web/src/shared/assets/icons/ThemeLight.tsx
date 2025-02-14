import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const ThemeLight = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)}>
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 20 20"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="xMidYMid meet"
      >
        <path
          d="M10 2v1m5.657 1.343-.707.707M18 10h-1m-1.343 5.657-.707-.707M10 17v1m-4.95-3.05-.707.707M3 10H2m3.05-4.95-.707-.707M14 10a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </div>
  );
};

export default ThemeLight;
