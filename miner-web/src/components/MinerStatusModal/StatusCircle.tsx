import clsx from "clsx";
import { ConcentricCircles } from "icons";

interface StatusCircleProps {
  isError?: boolean;
  isWarning?: boolean;
  width?: string;
}

const StatusCircle = ({ isError, isWarning, width }: StatusCircleProps) => {
  return (
    <ConcentricCircles
      className={clsx("mr-1", {
        "text-intent-success-fill": !isWarning && !isError,
        "text-intent-warning-fill": isWarning,
        "text-intent-critical-fill": isError,
      })}
      width={width}
    />
  );
};

export default StatusCircle;
