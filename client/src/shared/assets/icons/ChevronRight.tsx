import { iconSizes } from "./constants";
import InteractiveIcon from "./InteractiveIcon";
import { IconProps } from "./types";

const ChevronRight = ({ width = iconSizes.small, ...props }: IconProps) => {
  return (
    <InteractiveIcon {...props} width={width}>
      <svg width="100%" height="100%" viewBox="0 0 10 10" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="M2.80317 1.13642C2.51027 1.42931 2.51027 1.90418 2.80317 2.19708L5.60617 5.00008L2.80317 7.80308C2.51027 8.09598 2.51027 8.57085 2.80317 8.86374C3.09606 9.15664 3.57093 9.15664 3.86383 8.86374L7.19716 5.53041C7.49005 5.23752 7.49005 4.76264 7.19716 4.46975L3.86383 1.13642C3.57093 0.843525 3.09606 0.843525 2.80317 1.13642Z"
          fill="currentColor"
          fillOpacity="1"
        />
      </svg>
    </InteractiveIcon>
  );
};

export default ChevronRight;
