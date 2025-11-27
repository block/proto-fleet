import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

const Bitcoin = ({ className, width = iconSizes.small }: IconProps) => {
  return (
    <div className={clsx(width, className)} data-testid="bitcoin-icon">
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 16 16"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="xMidYMid meet"
      >
        <rect width="16" height="16" fill="currentColor" fillOpacity=".1" rx="8" />
        <path
          fill="currentColor"
          fillOpacity=".5"
          d="M8.818 7.202h-1.75l.343-1.552h1.482c.606 0 .956.277.956.734 0 .542-.425.818-1.03.818Zm-.627 3.157h-1.82l.401-1.818h1.483c.734 0 1.18.308 1.18.861 0 .563-.457.957-1.244.957Zm3.37-4.24c0-1.011-.77-1.667-1.827-1.857l.184-.874a.302.302 0 0 0-.295-.364H8.509c-.143 0-.266.099-.295.239l-.201.942h-1.83a.226.226 0 0 0-.22.177l-1.577 7.136a.226.226 0 0 0 .221.275h1.721l-.172.81a.302.302 0 0 0 .293.365l1.115.008c.144.001.269-.1.298-.24l.198-.943h.174c1.785 0 2.922-.977 2.922-2.242 0-.744-.403-1.35-1.03-1.658.786-.223 1.434-.85 1.434-1.775Z"
        />
      </svg>
    </div>
  );
};

export default Bitcoin;
