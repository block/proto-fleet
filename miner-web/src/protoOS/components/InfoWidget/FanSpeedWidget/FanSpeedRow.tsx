import clsx from "clsx";

// import Button, { sizes, variants } from "@/components/Button";
import FanSpeedPieChart from "./FanSpeedPieChart";
import Row from "@/shared/components/Row";

// import { ArrowRight } from "icons";
// import { iconSizes } from "icons/constants";

interface FanSpeedRowProps {
  acceptableSpeed: number;
  divider?: boolean;
  label: string;
  maxSpeed: number;
  secondaryLabel: string;
  speed: number;
  warn?: boolean;
}

const FanSpeedRow = ({
  acceptableSpeed,
  divider = true,
  label,
  maxSpeed,
  secondaryLabel,
  speed,
  warn,
}: FanSpeedRowProps) => {
  return (
    <Row className="flex space-x-4 items-center" divider={divider}>
      <div className="w-10 h-10">
        <FanSpeedPieChart
          acceptableSpeed={acceptableSpeed}
          fanSpeed={speed}
          maxSpeed={maxSpeed}
        />
      </div>
      <div className="grow">
        <div className="text-emphasis-300">{label}</div>
        <div className={clsx("text-200", { "text-intent-warning-text": warn })}>
          {secondaryLabel}
        </div>
      </div>
      {/* {warn && (
        // TODO: add onClick handler when repair manual is ready
        <Button
          size={sizes.compact}
          variant={variants.secondary}
          text="Repair instructions"
          suffixIcon={<ArrowRight className={iconSizes.small} />}
        />
      )} */}
    </Row>
  );
};

export default FanSpeedRow;
