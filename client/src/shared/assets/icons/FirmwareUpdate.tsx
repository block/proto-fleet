import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const FirmwareUpdate = ({ className, width = iconSizes.medium }: IconProps) => {
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
          d="M18 10L14 3.07H6L2 10L6 16.93H14L18 10Z"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinejoin="round"
        />
        <circle cx="10" cy="10" r="3.5" stroke="currentColor" strokeWidth="2" />
      </svg>
    </div>
  );
};

export default FirmwareUpdate;
