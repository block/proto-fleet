import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Immersion = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="immersion-icon">
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 20 20"
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        preserveAspectRatio="xMidYMid meet"
      >
        <path
          d="M2.5 16.7262C2.95 17.1131 3.4 17.5 4.375 17.5C6.25 17.5 6.25 15.9524 8.125 15.9524C9.1 15.9524 9.55 16.3393 10 16.7262C10.45 17.1131 10.9 17.5 11.875 17.5C13.75 17.5 13.75 15.9524 15.625 15.9524C16.6 15.9524 17.05 16.3393 17.5 16.7262M2.5 12.9167C2.95 13.3036 3.4 13.6905 4.375 13.6905C6.25 13.6905 6.25 12.1429 8.125 12.1429C9.1 12.1429 9.55 12.5298 10 12.9167C10.45 13.3036 10.9 13.6905 11.875 13.6905C13.75 13.6905 13.75 12.1429 15.625 12.1429C16.6 12.1429 17.05 12.5298 17.5 12.9167M15.8333 9.16667V5.16667C15.8333 4.23325 15.8333 3.76654 15.6517 3.41002C15.4919 3.09641 15.2369 2.84144 14.9233 2.68166C14.5668 2.5 14.1001 2.5 13.1667 2.5H7.04167C6.10825 2.5 5.64154 2.5 5.28502 2.68166C4.97141 2.84144 4.71644 3.09641 4.55666 3.41002C4.375 3.76654 4.375 4.23325 4.375 5.16667V10M7.5 5.83333H12.5"
          stroke="currentColor"
          strokeOpacity="0.9"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </div>
  );
};

export default Immersion;
