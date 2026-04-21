import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Alert = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="alert-icon">
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 20 20"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="xMidYMid meet"
      >
        <path
          d="M3.322 7.683C5.735 3.412 6.942 1.277 8.598.727a4.445 4.445 0 0 1 2.804 0c1.656.55 2.863 2.685 5.276 6.956 2.414 4.27 3.62 6.406 3.259 8.146-.2.958-.69 1.826-1.402 2.48C17.241 19.5 14.827 19.5 10 19.5s-7.241 0-8.535-1.19a4.658 4.658 0 0 1-1.402-2.48c-.362-1.74.845-3.876 3.259-8.147Z"
          fill="currentColor"
        />
        <path
          d="M9.992 14H10M10 11V7"
          className="stroke-surface-base"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </div>
  );
};

export default Alert;
