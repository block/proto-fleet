import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Hashrate = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="hashrate-icon">
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 20 20"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="xMidYMid meet"
      >
        <path
          stroke="#000"
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeOpacity=".9"
          strokeWidth="2"
          d="M10.624 6.85 8.743 9.134c-.202.244-.058.596.271.662l1.972.39c.35.07.484.46.239.697L8.89 13.15M19 7.75h-1.8m-14.4 4.5H1m1.8-4.5H1m18 4.5h-1.8m-4.95 4.95V19m-4.5-1.8V19m4.5-18v1.8M7.75 1v1.8M2.8 10c0-3.394 0-5.091 1.054-6.146C4.91 2.8 6.606 2.8 10 2.8s5.091 0 6.146 1.054C17.2 4.91 17.2 6.606 17.2 10s0 5.091-1.054 6.146C15.09 17.2 13.394 17.2 10 17.2s-5.091 0-6.146-1.054C2.8 15.09 2.8 13.394 2.8 10Z"
        />
      </svg>
    </div>
  );
};

export default Hashrate;
