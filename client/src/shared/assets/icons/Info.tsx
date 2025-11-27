import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Info = ({ className, width = iconSizes.medium }: IconProps) => {
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
        <path className="fill-surface-base" fillOpacity=".01" d="M0 0h20v20H0z" />
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="M19 10a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-8-4.25a1 1 0 1 1-2 0 1 1 0 0 1 2 0Zm0 8.5v-6H9v6h2Z"
          fill="currentColor"
        />
      </svg>
    </div>
  );
};

export default Info;
