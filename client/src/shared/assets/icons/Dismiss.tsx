import { iconSizes } from "./constants";
import InteractiveIcon from "./InteractiveIcon";
import { IconProps } from "./types";

const Dismiss = ({ opacity = ".01", width = iconSizes.medium, ...props }: IconProps) => {
  return (
    <InteractiveIcon width={width} {...props}>
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 20 20"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        className={props.className}
        preserveAspectRatio="xMidYMid meet"
      >
        <path className="fill-surface-base" fillOpacity={opacity} d="M0 0h20v20H0z" />
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="m3 1.586.707.707L10 8.586l6.293-6.293.707-.707L18.414 3l-.707.707L11.414 10l6.293 6.293.707.707L17 18.414l-.707-.707L10 11.414l-6.293 6.293-.707.707L1.586 17l.707-.707L8.586 10 2.293 3.707 1.586 3 3 1.586Z"
          fill="currentColor"
        />
      </svg>
    </InteractiveIcon>
  );
};

export default Dismiss;
