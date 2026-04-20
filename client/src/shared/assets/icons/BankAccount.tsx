import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const BankAccount = ({ className, width = iconSizes.small }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="bank-account-icon">
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 16 16"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="xMidYMid meet"
      >
        <path
          fill="currentColor"
          fillOpacity=".9"
          fillRule="evenodd"
          d="M8.124 2.935a.25.25 0 0 0-.248 0L3.824 5.25h8.352L8.124 2.935ZM12.25 6.75h-3.5v6h3.5v-6Zm.75 7.5h2a.75.75 0 0 0 0-1.5h-1.25v-6h.873c.872 0 1.179-1.155.422-1.588l-6.177-3.53a1.75 1.75 0 0 0-1.736 0L.955 5.162c-.757.433-.45 1.588.422 1.588h.873v6H1a.75.75 0 0 0 0 1.5h12Zm-5.75-1.5v-6h-3.5v6h3.5Z"
          clipRule="evenodd"
        />
      </svg>
    </div>
  );
};

export default BankAccount;
