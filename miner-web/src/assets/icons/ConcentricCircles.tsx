import { IconProps } from "./types";

const ConcentricCircles = ({ className }: IconProps) => {
  return (
    <svg
      width="12"
      height="12"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={className}
      data-testid="concentric-circles"
    >
      <circle
        opacity=".4"
        cx="6"
        cy="6"
        r="5.5"
        stroke="currentColor"
        strokeOpacity=".8"
      />
      <circle
        cx="6"
        cy="6"
        r="3"
        fill="currentColor"
        fillOpacity=".8"
      />
    </svg>
  );
};

export default ConcentricCircles;
