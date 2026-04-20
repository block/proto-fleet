import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const ArrowUp = ({ className, width = iconSizes.small }: IconProps) => {
  return (
    <div className={clsx(width, className)}>
      <svg width="100%" height="100%" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path
          d="M9.31643 4.27055C9.70104 3.91003 10.299 3.91003 10.6836 4.27055L14.6836 8.02055C15.0865 8.39821 15.1071 9.03071 14.7295 9.43357C14.3518 9.83644 13.7193 9.85707 13.3164 9.47948L11 7.30768L11 15C11 15.5523 10.5523 16 10 16C9.44774 16 9.00002 15.5523 9.00002 15L9.00002 7.30768L6.68362 9.47948C6.2807 9.85707 5.64821 9.83644 5.27053 9.43357C4.89296 9.03071 4.91357 8.39821 5.31643 8.02055L9.31643 4.27055Z"
          fill="currentColor"
        />
      </svg>
    </div>
  );
};

export default ArrowUp;
