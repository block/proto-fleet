import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Building = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="building-icon">
      <svg width="20" height="20" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="M17 0C18.6569 0 20 1.34315 20 3V17C20 18.6569 18.6569 20 17 20H3C1.34315 20 0 18.6569 0 17V3C0 1.34315 1.34315 0 3 0H17ZM2 11H3C3.55228 11 4 11.4477 4 12C4 12.5523 3.55228 13 3 13H2V17C2 17.5523 2.44772 18 3 18H9V13H8C7.44772 13 7 12.5523 7 12C7 11.4477 7.44772 11 8 11H9V6H2V11ZM11 11H12C12.5523 11 13 11.4477 13 12C13 12.5523 12.5523 13 12 13H11V18H17C17.5523 18 18 17.5523 18 17V13H17C16.4477 13 16 12.5523 16 12C16 11.4477 16.4477 11 17 11H18V6H11V11ZM3 2C2.44772 2 2 2.44772 2 3V4H18V3C18 2.44772 17.5523 2 17 2H3Z"
          fill="currentColor"
          fillOpacity="0.9"
        />
      </svg>
    </div>
  );
};

export default Building;
