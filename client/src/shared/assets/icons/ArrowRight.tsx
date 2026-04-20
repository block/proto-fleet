import { iconSizes } from "./constants";
import { IconProps } from "./types";

const ArrowRight = ({ className, width = iconSizes.medium }: IconProps) => {
  return (
    <div className={width}>
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 20 20"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        className={className}
        preserveAspectRatio="xMidYMid meet"
      >
        <path fill="currentColor" fillOpacity=".01" d="M0 0h20v20H0z" />
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="m10.586 16 .707-.707L15.586 11H1V9h14.586l-4.293-4.293L10.586 4 12 2.586l.707.707 6 6a1 1 0 0 1 0 1.414l-6 6-.707.707L10.586 16Z"
          fill="currentColor"
        />
      </svg>
    </div>
  );
};

export default ArrowRight;
