import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const InfoInverted = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)}>
      <svg xmlns="http://www.w3.org/2000/svg" width="100%" height="100%" viewBox="0 0 20 20" fill="none">
        <rect width="62.5%" height="62.5%" className="fill-surface-base" fillOpacity="0.01" />
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="M10 2C5.58172 2 2 5.58172 2 10C2 14.4183 5.58172 18 10 18C14.4183 18 18 14.4183 18 10C18 5.58172 14.4183 2 10 2ZM0 10C0 4.47715 4.47715 0 10 0C15.5228 0 20 4.47715 20 10C20 15.5228 15.5228 20 10 20C4.47715 20 0 15.5228 0 10ZM8.99198 6C8.99198 5.44772 9.43969 5 9.99198 5H10.001C10.5533 5 11.001 5.44772 11.001 6C11.001 6.55228 10.5533 7 10.001 7H9.99198C9.43969 7 8.99198 6.55228 8.99198 6ZM9.99292 8C10.5452 8 10.9929 8.44772 10.9929 9V13C10.9929 13.5523 10.5452 14 9.99292 14C9.44063 14 8.99292 13.5523 8.99292 13V9C8.99292 8.44772 9.44063 8 9.99292 8Z"
          fill="currentColor"
        />
      </svg>
    </div>
  );
};

export default InfoInverted;
