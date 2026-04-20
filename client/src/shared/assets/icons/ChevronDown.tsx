import { iconSizes } from "./constants";
import InteractiveIcon from "./InteractiveIcon";
import { IconProps } from "./types";

const ChevronDown = ({ width = iconSizes.small, ...props }: IconProps) => {
  return (
    <InteractiveIcon {...props} width={width}>
      <svg width="100%" height="100%" viewBox="0 0 10 10" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="M8.86374 2.80317C8.57085 2.51027 8.09598 2.51027 7.80308 2.80317L5.00008 5.60617L2.19708 2.80317C1.90418 2.51027 1.42931 2.51027 1.13642 2.80317C0.843525 3.09606 0.843525 3.57093 1.13642 3.86383L4.46975 7.19716C4.76264 7.49005 5.23752 7.49005 5.53041 7.19716L8.86374 3.86383C9.15664 3.57093 9.15664 3.09606 8.86374 2.80317Z"
          fill="currentColor"
          fillOpacity="1"
        />
      </svg>
    </InteractiveIcon>
  );
};

export default ChevronDown;
