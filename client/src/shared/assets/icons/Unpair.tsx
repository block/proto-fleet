import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Unpair = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="unpair-icon">
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
          d="M10 2a8 8 0 1 0 0 16 8 8 0 0 0 0-16ZM0 10C0 4.477 4.477 0 10 0s10 4.477 10 10-4.477 10-10 10S0 15.523 0 10Zm6.707-3.293a1 1 0 0 1 1.414 0L10 8.586l1.879-1.879a1 1 0 0 1 1.414 1.414L11.414 10l1.879 1.879a1 1 0 0 1-1.414 1.414L10 11.414l-1.879 1.879a1 1 0 0 1-1.414-1.414L8.586 10 6.707 8.121a1 1 0 0 1 0-1.414Z"
          clipRule="evenodd"
        />
      </svg>
    </div>
  );
};

export default Unpair;
