import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Stop = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)}>
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 32 32"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="xMidYMid meet"
      >
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="M11.858 0a6 6 0 0 0-4.243 1.757L1.757 7.615A6 6 0 0 0 0 11.858v8.284a6 6 0 0 0 1.757 4.243l5.858 5.858A6 6 0 0 0 11.858 32h8.284a6 6 0 0 0 4.243-1.757l5.858-5.858A6 6 0 0 0 32 20.142v-8.284a6 6 0 0 0-1.757-4.243l-5.858-5.858A6 6 0 0 0 20.142 0h-8.284ZM17.6 10.4a1.6 1.6 0 1 0-3.2 0v6.4a1.6 1.6 0 1 0 3.2 0v-6.4ZM15.987 20a1.6 1.6 0 0 0 0 3.2h.014a1.6 1.6 0 1 0 0-3.2h-.014Z"
          fill="currentColor"
        />
      </svg>
    </div>
  );
};

export default Stop;
