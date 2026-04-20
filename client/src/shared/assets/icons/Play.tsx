import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Play = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)}>
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 20 21"
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        preserveAspectRatio="xMidYMid meet"
      >
        <path
          stroke="currentColor"
          strokeLinejoin="round"
          strokeWidth="2"
          d="M17.875 11.015c-.404 1.612-2.313 2.75-6.131 5.028-3.691 2.202-5.537 3.303-7.024 2.86a3.686 3.686 0 0 1-1.627-1.009C2 16.737 2 14.491 2 10s0-6.737 1.093-7.894a3.686 3.686 0 0 1 1.627-1.01c1.487-.442 3.333.659 7.024 2.86 3.818 2.278 5.727 3.417 6.13 5.029a4.182 4.182 0 0 1 0 2.03Z"
        />
      </svg>
    </div>
  );
};

export default Play;
