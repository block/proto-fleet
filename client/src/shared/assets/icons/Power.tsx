import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const ConcentricCircles = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)}>
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 20 20"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="xMidYMid meet"
      >
        <path d="M0 0h20v20H0z" />
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="M10.004 1.001a1 1 0 0 1 1 1V9a1 1 0 1 1-2 0V2a1 1 0 0 1 1-1Zm-4.96 3.464a1 1 0 0 1 .01 1.414 7.129 7.129 0 0 0-1.92 3.636 7.19 7.19 0 0 0 .4 4.107 7.078 7.078 0 0 0 2.582 3.184A6.932 6.932 0 0 0 10 17.999a6.932 6.932 0 0 0 3.884-1.193 7.078 7.078 0 0 0 2.581-3.184 7.189 7.189 0 0 0 .4-4.107 7.129 7.129 0 0 0-1.919-3.636 1 1 0 1 1 1.423-1.406 9.129 9.129 0 0 1 2.459 4.657 9.19 9.19 0 0 1-.512 5.25 9.078 9.078 0 0 1-3.311 4.083A8.931 8.931 0 0 1 10 19.999a8.931 8.931 0 0 1-5.004-1.536 9.077 9.077 0 0 1-3.312-4.084 9.19 9.19 0 0 1-.512-5.25 9.129 9.129 0 0 1 2.459-4.656 1 1 0 0 1 1.414-.008Z"
          fill="currentColor"
        />
      </svg>
    </div>
  );
};

export default ConcentricCircles;
