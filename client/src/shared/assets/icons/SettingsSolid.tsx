import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const SettingsSolid = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)}>
      <svg xmlns="http://www.w3.org/2000/svg" width="100%" height="100%" viewBox="0 0 20 20" fill="none">
        <path
          d="M13.0005 1.80383C14.0722 1.80398 15.0623 2.37575 15.5982 3.30383L18.5982 8.50012C19.134 9.42831 19.1341 10.5719 18.5982 11.5001L15.5982 16.6964C15.0623 17.6244 14.0721 18.1963 13.0005 18.1964H7.00051C5.92876 18.1964 4.93779 17.6245 4.40188 16.6964L1.40188 11.5001C0.866011 10.572 0.86607 9.4283 1.40188 8.50012L4.40188 3.30383C4.93778 2.37563 5.92872 1.80383 7.00051 1.80383H13.0005ZM10.0005 7.00012C8.34366 7.00012 7.00051 8.34327 7.00051 10.0001C7.00058 11.6569 8.3437 13.0001 10.0005 13.0001C11.6571 12.9999 13.0004 11.6568 13.0005 10.0001C13.0005 8.3434 11.6572 7.00034 10.0005 7.00012Z"
          fill="currentColor"
          fillOpacity="0.9"
        />
      </svg>
    </div>
  );
};

export default SettingsSolid;
