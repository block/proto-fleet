import SkeletonBar from "@/shared/components/SkeletonBar";
import { separateByCommas } from "@/shared/utils/stringUtils";

interface HashRateValueProps {
  value: number | undefined | null;
}

function HashRateValue({ value }: HashRateValueProps) {
  if (value === null) {
    return <>N/A</>;
  }

  if (value === undefined) {
    return <SkeletonBar />;
  }

  // Convert TH/s to PH/s if value > 1000
  if (value > 1000) {
    const displayValue = value / 1000;
    return <>{separateByCommas(displayValue.toFixed(1))} PH/s</>;
  }

  return <>{separateByCommas(value.toFixed(1))} TH/s</>;
}

export default HashRateValue;
