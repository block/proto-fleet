import { iconSizes } from "./constants";
import { IconProps } from "./types";

const C1Chip = ({ width = iconSizes.medium }: IconProps) => {
  return (
    <div className={width}>
      <svg width="100%" height="100%" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path
          d="M9.9069 7.3031H7.54219C6.33665 7.3031 5.35938 8.28038 5.35938 9.48591V10.2135C5.35938 11.4191 6.33665 12.3963 7.54219 12.3963H9.9069"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
          shapeRendering="crispEdges"
        />
        <g opacity="0.5">
          <path
            d="M14.0526 12.3963V7.3031C13.6888 8.0307 12.7793 8.39451 12.1123 8.39451"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </g>
        <rect x="0.5" y="0.5" width="19" height="19" rx="3.5" stroke="currentColor" strokeOpacity="0.1" />
      </svg>
    </div>
  );
};

export default C1Chip;
