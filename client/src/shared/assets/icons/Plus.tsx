import clsx from "clsx";
import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Plus = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="plus-icon">
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 20 20"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="xMidYMid meet"
      >
        <path
          fill="currentColor"
          fillRule="evenodd"
          d="M9 0a1 1 0 0 1 1 1v7h7a1 1 0 1 1 0 2h-7v7a1 1 0 1 1-2 0v-7H1a1 1 0 1 1 0-2h7V1a1 1 0 0 1 1-1Z"
          clipRule="evenodd"
        />
      </svg>
    </div>
  );
};

export default Plus;
