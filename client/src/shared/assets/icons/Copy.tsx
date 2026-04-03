import clsx from "clsx";
import { iconSizes } from "./constants";
import type { IconProps } from "./types";

const Copy = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="copy-icon">
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 24 24"
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        preserveAspectRatio="xMidYMid meet"
      >
        <path
          d="M20 7C20.2652 7 20.5195 7.10544 20.707 7.29297C20.8946 7.4805 21 7.73479 21 8V21C21 21.5523 20.5523 22 20 22H9C8.44772 22 8 21.5523 8 21V8L8.00488 7.89746C8.05622 7.39334 8.48233 7 9 7H20ZM10 20H19V9H10V20ZM16 4H5V17H3V3C3 2.73478 3.10543 2.48051 3.29297 2.29297C3.48051 2.10543 3.73478 2 4 2H16V4Z"
          fill="currentColor"
        />
      </svg>
    </div>
  );
};

export default Copy;
