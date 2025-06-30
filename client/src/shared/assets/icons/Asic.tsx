import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Asic = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="asic-icon">
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
          fillOpacity=".9"
          fillRule="evenodd"
          d="M6.5 0a1 1 0 0 1 1 1v1H9V1a1 1 0 1 1 2 0v1h1.5V1a1 1 0 1 1 2 0v1.031l.022.003a4 4 0 0 1 3.444 3.444l.003.022H19a1 1 0 1 1 0 2h-1V9h1a1 1 0 1 1 0 2h-1v1.5h1a1 1 0 1 1 0 2h-1.031l-.003.022a4 4 0 0 1-3.444 3.444l-.022.003V19a1 1 0 1 1-2 0v-1H11v1a1 1 0 1 1-2 0v-1H7.5v1a1 1 0 1 1-2 0v-1.031l-.022-.003A4 4 0 0 1 2.03 14.5H1a1 1 0 1 1 0-2h1V11H1a1 1 0 1 1 0-2h1V7.5H1a1 1 0 0 1 0-2h1.031l.003-.022A4 4 0 0 1 5.5 2.03V1a1 1 0 0 1 1-1Zm0 4c-.496 0-.647.002-.761.017A2 2 0 0 0 4.017 5.74C4.002 5.853 4 6.004 4 6.5v7c0 .496.002.648.017.761a2 2 0 0 0 1.722 1.722c.114.015.265.017.761.017h7c.496 0 .648-.002.761-.017a2 2 0 0 0 1.722-1.722c.015-.113.017-.265.017-.761v-7c0-.496-.002-.647-.017-.761a2 2 0 0 0-1.722-1.722C14.148 4.002 13.996 4 13.5 4h-7Z"
          clipRule="evenodd"
        />
      </svg>
    </div>
  );
};

export default Asic;
