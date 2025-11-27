import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const ArrowLeftCompact = ({ className, width = iconSizes.medium }: IconProps) => {
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
          d="M1 0a1 1 0 0 1 1 1v18a1 1 0 1 1-2 0V1a1 1 0 0 1 1-1Zm11.169 4.257a1 1 0 0 1 .074 1.412L9.245 9H19a1 1 0 1 1 0 2H9.245l2.998 3.331a1 1 0 0 1-1.486 1.338l-4.5-5a1 1 0 0 1 0-1.338l4.5-5a1 1 0 0 1 1.412-.074Z"
          clipRule="evenodd"
        />
      </svg>
    </div>
  );
};

export default ArrowLeftCompact;
