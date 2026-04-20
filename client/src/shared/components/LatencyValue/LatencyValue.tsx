import SkeletonBar from "@/shared/components/SkeletonBar";

interface LatencyValueProps {
  value: number | undefined | null;
}

function LatencyValue({ value }: LatencyValueProps) {
  if (value === null) {
    return <>N/A</>;
  }

  if (value === undefined) {
    return <SkeletonBar />;
  }

  return <>{value.toFixed(1)} ms</>;
}

export default LatencyValue;
