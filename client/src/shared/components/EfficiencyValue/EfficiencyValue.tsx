import SkeletonBar from "@/shared/components/SkeletonBar";
import { separateByCommas } from "@/shared/utils/stringUtils";

interface EfficiencyValueProps {
  value: number | undefined | null;
}

function EfficiencyValue({ value }: EfficiencyValueProps) {
  if (value === null) {
    return <>N/A</>;
  }

  if (value === undefined) {
    return <SkeletonBar />;
  }

  return <>{separateByCommas(value.toFixed(1))} J/TH</>;
}

export default EfficiencyValue;
