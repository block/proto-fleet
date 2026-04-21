import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Curtail = ({ className, width = iconSizes.medium }: IconProps) => {
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
          fill="currentColor"
          fillRule="evenodd"
          d="M10 2a8 8 0 1 0 0 16 8 8 0 0 0 0-16ZM0 10C0 4.477 4.477 0 10 0s10 4.477 10 10-4.477 10-10 10S0 15.523 0 10Zm7.75-3.7a1 1 0 0 1 1 1v5.4a1 1 0 1 1-2 0V7.3a1 1 0 0 1 1-1Zm4.5 0a1 1 0 0 1 1 1v5.4a1 1 0 1 1-2 0V7.3a1 1 0 0 1 1-1Z"
          clipRule="evenodd"
        />
      </svg>
    </div>
  );
};

export default Curtail;
