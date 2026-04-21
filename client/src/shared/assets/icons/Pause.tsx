import InteractiveIcon from "./InteractiveIcon";
import { IconProps } from "./types";

const Pause = (props: IconProps) => {
  return (
    <InteractiveIcon {...props}>
      <svg width="20" height="20" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path
          transform="rotate(90 19.6 .4)"
          className="stroke-surface-base"
          strokeOpacity=".01"
          strokeWidth=".8"
          d="M19.6.4h19.2v19.2H19.6z"
        />
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="M17 14a1 1 0 0 1-1 1H4a1 1 0 1 1 0-2h12a1 1 0 0 1 1 1Zm0-8a1 1 0 0 1-1 1H4a1 1 0 1 1 0-2h12a1 1 0 0 1 1 1Z"
          fill="currentColor"
        />
      </svg>
    </InteractiveIcon>
  );
};

export default Pause;
