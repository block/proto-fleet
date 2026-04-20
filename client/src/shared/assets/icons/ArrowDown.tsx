import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const ArrowDown = ({ className, width = iconSizes.small }: IconProps) => {
  return (
    <div className={clsx(width, className)}>
      <svg width="100%" height="100%" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path
          d="M10.6836 15.7294C10.299 16.0899 9.70104 16.0899 9.31643 15.7294L5.31643 11.9794C4.91357 11.6018 4.89296 10.9693 5.27053 10.5664C5.64821 10.1635 6.2807 10.1429 6.68362 10.5205L9.00002 12.6923L9.00002 4.99995C9.00002 4.44767 9.44774 3.99995 10 3.99995C10.5523 3.99995 11 4.44767 11 4.99995L11 12.6923L13.3164 10.5205C13.7193 10.1429 14.3518 10.1635 14.7295 10.5664C15.1071 10.9693 15.0865 11.6018 14.6836 11.9794L10.6836 15.7294Z"
          fill="currentColor"
        />
      </svg>
    </div>
  );
};

export default ArrowDown;
