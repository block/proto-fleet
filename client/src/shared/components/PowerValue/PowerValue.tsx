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

  // Always display power in watts (W)
  return <>{separateByCommas(Math.round(value))} W</>;
}

export default PowerValue;
