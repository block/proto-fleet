import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Reboot = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)}>
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 18 18"
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        preserveAspectRatio="xMidYMid meet"
      >
        <path
          fill="currentColor"
          fillRule="evenodd"
          d="M9.668.293a1 1 0 0 1 1.414 0l3.125 3.125a1 1 0 0 1 0 1.414l-3.125 3.125a1 1 0 1 1-1.414-1.414l1.418-1.418H5a3 3 0 0 0-3 3v2.25a1 1 0 1 1-2 0v-2.25a5 5 0 0 1 5-5h6.086L9.668 1.707a1 1 0 0 1 0-1.414ZM16.5 5.75a1 1 0 0 1 1 1V9a5 5 0 0 1-5 5H6.414l1.418 1.418a1 1 0 1 1-1.414 1.414l-3.125-3.125a1 1 0 0 1 0-1.414l3.125-3.125a1 1 0 1 1 1.414 1.414L6.414 12H12.5a3 3 0 0 0 3-3V6.75a1 1 0 0 1 1-1Z"
          clipRule="evenodd"
        />
      </svg>
    </div>
  );
};

export default Reboot;
