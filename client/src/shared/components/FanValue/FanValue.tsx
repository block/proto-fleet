import SkeletonBar from "@/shared/components/SkeletonBar";
import { separateByCommas } from "@/shared/utils/stringUtils";

interface FanValueProps {
  value: number | undefined | null;
  type: "rpm" | "pwm";
}

function FanValue({ value, type }: FanValueProps) {
  if (value === null) {
    return <>N/A</>;
  }

  if (value === undefined) {
    return <SkeletonBar />;
  }

  if (type === "rpm") {
    return <>{separateByCommas(value)} RPM</>;
  }

  // type === "pwm"
  return <>{value.toFixed(1)}% PWM</>;
}

export default FanValue;
