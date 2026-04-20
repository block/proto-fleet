import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Racks = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="racks-icon">
      <svg width="18" height="18" viewBox="0 0 18 18" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path
          d="M16 0C17.1046 0 18 0.895431 18 2V17C18 17.5523 17.5523 18 17 18C16.4477 18 16 17.5523 16 17V16H2V17C2 17.5523 1.55228 18 1 18C0.447715 18 0 17.5523 0 17V2C0 0.895431 0.895431 0 2 0H16ZM2 14H16V9H2V14ZM2 7H16V2H2V7Z"
          fill="currentColor"
          fillOpacity="0.9"
        />
      </svg>
    </div>
  );
};

export default Racks;
