import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Notification = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="notification-icon">
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 20 20"
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        preserveAspectRatio="xMidYMid meet"
      >
        <path
          fill="currentColor"
          fillOpacity=".9"
          fillRule="evenodd"
          d="M2.975 5.68a7.185 7.185 0 0 1 14.05 0l.453 2.11.67 3.13.01.042c.134.627.248 1.16.303 1.6.056.457.066.926-.09 1.392a3 3 0 0 1-1.316 1.627c-.423.25-.883.34-1.343.38-.441.039-.987.039-1.627.039H5.915c-.64 0-1.185 0-1.627-.039-.46-.04-.92-.13-1.342-.38a3 3 0 0 1-1.316-1.627c-.157-.466-.147-.935-.09-1.393.054-.44.168-.973.303-1.599l.009-.042.67-3.13.453-2.11ZM10 2a5.185 5.185 0 0 0-5.07 4.099l-.452 2.11-.67 3.13c-.146.68-.241 1.127-.284 1.469-.041.334-.016.456.002.51a1 1 0 0 0 .439.543c.048.028.163.079.498.108.344.03.8.031 1.495.031h8.084c.695 0 1.152 0 1.495-.031.336-.03.45-.08.499-.108a1 1 0 0 0 .438-.543c.018-.054.043-.176.002-.51-.043-.342-.137-.789-.283-1.469l-.67-3.13-.453-2.11A5.185 5.185 0 0 0 10 2ZM6.051 18.184a1 1 0 0 1 1.265-.633c1.742.581 3.626.581 5.368 0a1 1 0 0 1 .632 1.898c-2.152.717-4.48.717-6.632 0a1 1 0 0 1-.633-1.265Z"
          clipRule="evenodd"
        />
      </svg>
    </div>
  );
};

export default Notification;
