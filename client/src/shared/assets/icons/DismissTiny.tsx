import InteractiveIcon from "./InteractiveIcon";
import { IconProps } from "./types";

const DismissCircle = ({ opacity, ...props }: IconProps) => {
  return (
    <InteractiveIcon {...props}>
      <svg width="16" height="16" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path className="fill-surface-base" fillOpacity=".02" d="M0 0h16v16H0z" />
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="M3.933 3.933a1 1 0 0 1 1.414 0L8 6.586l2.652-2.653a1 1 0 0 1 1.415 1.414L9.414 8l2.653 2.653a1 1 0 0 1-1.415 1.414L8 9.414l-2.653 2.653a1 1 0 0 1-1.414-1.414L6.585 8 3.933 5.347a1 1 0 0 1 0-1.414Z"
          fill="currentColor"
          fillOpacity={opacity}
        />
      </svg>
    </InteractiveIcon>
  );
};

export default DismissCircle;
