import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Lightning = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)}>
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 12 12"
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        preserveAspectRatio="xMidYMid meet"
      >
        <path
          fill="currentColor"
          fillRule="evenodd"
          d="M6.874.149a.665.665 0 0 1 .138.879L5.057 4.025l1.3.433 1.341.447 1.393.464a.665.665 0 0 1 .26 1.101L7.41 8.41l-3.396 3.395a.665.665 0 0 1-1.027-.833l1.955-2.997-.396-.132-1.212-.404L.91 6.63A.665.665 0 0 1 .65 5.53l1.94-1.94L5.985.195c.24-.241.625-.261.889-.046Z"
          clipRule="evenodd"
        />
      </svg>
    </div>
  );
};

export default Lightning;
