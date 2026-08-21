import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const MinusFilled = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="minus-filled-icon">
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 20 20"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="xMidYMid meet"
      >
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="M10 20C15.5228 20 20 15.5228 20 10C20 4.47715 15.5228 0 10 0C4.47715 0 0 4.47715 0 10C0 15.5228 4.47715 20 10 20ZM6 9C5.44772 9 5 9.44771 5 10C5 10.5523 5.44772 11 6 11H14C14.5523 11 15 10.5523 15 10C15 9.44771 14.5523 9 14 9H6Z"
          fill="currentColor"
          fillOpacity="0.9"
        />
      </svg>
    </div>
  );
};

export default MinusFilled;
