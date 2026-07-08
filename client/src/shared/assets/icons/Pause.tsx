import InteractiveIcon from "./InteractiveIcon";
import { IconProps } from "./types";

const Pause = (props: IconProps) => {
  return (
    <InteractiveIcon {...props}>
      <svg width="20" height="20" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path
          d="M7 4a1 1 0 0 1 1 1v10a1 1 0 1 1-2 0V5a1 1 0 0 1 1-1Zm6 0a1 1 0 0 1 1 1v10a1 1 0 1 1-2 0V5a1 1 0 0 1 1-1Z"
          fill="currentColor"
        />
      </svg>
    </InteractiveIcon>
  );
};

export default Pause;
