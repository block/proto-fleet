import SkeletonBar from "@/shared/components/SkeletonBar";
import { separateByCommas } from "@/shared/utils/stringUtils";
import { formatHashrateWithUnit } from "@/shared/utils/utility";

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

  const { value: displayValue, unit } = formatHashrateWithUnit(value);
  return (
    <>
      {separateByCommas(displayValue.toFixed(1))} {unit}
    </>
  );
}

export default HashRateValue;
