import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Site = ({ className, width = iconSizes.small }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="site-icon">
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 20 20"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="xMidYMid meet"
      >
        <path
          d="M10 0C12.3572 0 14.6355.848 16.3291 2.385 18.0258 3.924 19 6.034 19 8.257c0 3.254-2.271 6.194-4.297 8.199a24.211 24.211 0 0 1-2.85 2.457 15.26 15.26 0 0 1-.943.68l-.065.042-.026.016L10 19l.537.844a1.137 1.137 0 0 1-1.074 0L10 19l-.538.844-.003-.002-.027-.016a15.26 15.26 0 0 1-1.008-.722 24.211 24.211 0 0 1-2.85-2.457C3.27 14.45 1 11.512 1 8.257 1 6.034 1.974 3.924 3.671 2.385 5.365.848 7.643 0 10 0Zm0 2C8.114 2 6.322 2.68 5.015 3.866 3.711 5.049 3 6.631 3 8.257c0 2.39 1.729 4.823 3.703 6.778A22.218 22.218 0 0 0 10 17.79a22.218 22.218 0 0 0 3.297-2.754C15.271 13.08 17 10.647 17 8.257 17 6.631 16.289 5.049 14.985 3.866 13.678 2.681 11.886 2 10 2Zm0 3.929a2 2 0 1 1 0 4 2 2 0 0 1 0-4Z"
          fill="currentColor"
        />
      </svg>
    </div>
  );
};

export default Site;
