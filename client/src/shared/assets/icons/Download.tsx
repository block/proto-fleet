import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Download = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="download-icon">
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 19 19"
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        preserveAspectRatio="xMidYMid meet"
      >
        <path
          fill="currentColor"
          fillRule="evenodd"
          d="M10 .667a1 1 0 0 1 1 1v9.252l1.626-1.626a1 1 0 1 1 1.414 1.414l-3.333 3.334a1 1 0 0 1-1.415 0L5.96 10.707a1 1 0 1 1 1.414-1.414L9 10.92V1.667a1 1 0 0 1 1-1ZM4.88 3.349a1 1 0 0 1-.014 1.414 7.333 7.333 0 1 0 10.267 0 1 1 0 0 1 1.4-1.428 9.333 9.333 0 1 1-13.067 0 1 1 0 0 1 1.414.014Z"
          clipRule="evenodd"
        />
      </svg>
    </div>
  );
};

export default Download;
