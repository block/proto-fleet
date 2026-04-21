import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Speedometer = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)}>
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 20 20"
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        preserveAspectRatio="xMidYMid meet"
      >
        <path
          stroke="currentColor"
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth="2"
          d="M10 1v2.25M10 1a9 9 0 0 0-9 9m9-9a9 9 0 0 1 9 9m-9 6.75V19m0 0a9 9 0 0 0 9-9m-9 9a9 9 0 0 1-9-9m2.25 0H1m18 0h-2.25m-.38 6.37-1.596-1.596M3.63 16.371l1.598-1.598M3.63 3.7l1.563 1.563M16.371 3.7 11.35 8.65M11.8 10a1.8 1.8 0 1 1-3.6 0 1.8 1.8 0 0 1 3.6 0Z"
        />
      </svg>
    </div>
  );
};

export default Speedometer;
