import SkeletonBar from "@/shared/components/SkeletonBar";
import { separateByCommas } from "@/shared/utils/stringUtils";

interface PowerValueProps {
  value: number | undefined | null;
}

function PowerValue({ value }: PowerValueProps) {
  if (value === null) {
    return <>N/A</>;
  }

  if (value === undefined) {
    return <SkeletonBar />;
  }

  // Convert W to kW if value >= 1000
  if (value >= 1000) {
    const displayValue = value / 1000;
    return <>{separateByCommas(displayValue.toFixed(1))} kW</>;
  }

  return <>{separateByCommas(value.toFixed(0))} W</>;
}

export default PowerValue;
