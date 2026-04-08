import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Calendar = ({ className, onClick, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)} onClick={onClick}>
      <svg width="100%" height="100%" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="M6 1a1 1 0 0 1 1 1v1h6V2a1 1 0 1 1 2 0v1h1a3 3 0 0 1 3 3v10a3 3 0 0 1-3 3H4a3 3 0 0 1-3-3V6a3 3 0 0 1 3-3h1V2a1 1 0 0 1 1-1ZM3 9v7a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V9H3Zm14-2V6a1 1 0 0 0-1-1H4a1 1 0 0 0-1 1v1h14Z"
          fill="currentColor"
        />
      </svg>
    </div>
  );
};

export default Calendar;
