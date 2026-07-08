import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const PlusFilled = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="plus-filled-icon">
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
          d="M20 10C20 15.5228 15.5228 20 10 20C4.47715 20 0 15.5228 0 10C0 4.47715 4.47715 0 10 0C15.5228 0 20 4.47715 20 10ZM5 10C5 9.44771 5.44772 9 6 9H9V6C9 5.44772 9.44771 5 10 5C10.5523 5 11 5.44772 11 6V9H14C14.5523 9 15 9.44771 15 10C15 10.5523 14.5523 11 14 11H11V14C11 14.5523 10.5523 15 10 15C9.44771 15 9 14.5523 9 14V11H6C5.44772 11 5 10.5523 5 10Z"
          fill="currentColor"
          fillOpacity="0.9"
        />
      </svg>
    </div>
  );
};

export default PlusFilled;
