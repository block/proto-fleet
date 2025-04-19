import type { AsicStats } from "@/protoOS/api/types";
import { useAsicColor } from "@/protoOS/features/kpis/hooks";

const AsicCell = ({ asic }: { asic: AsicStats }) => {
  const backgroundColor = useAsicColor(asic);

  return (
    <div
      style={{ backgroundColor }}
      className="relative h-1.5 grow basis-0 rounded-xl border-1 border-core-primary-5"
    />
  );
};

export default AsicCell;
