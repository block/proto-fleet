import { useAsicColor } from "@/protoOS/features/kpis/hooks";
import type { AsicData } from "@/protoOS/store";

const AsicCell = ({ asic }: { asic: AsicData }) => {
  const backgroundColor = useAsicColor(asic);

  return (
    <div
      style={{ backgroundColor }}
      className="relative h-1.5 grow basis-0 rounded-xl border-1 border-core-primary-5"
    />
  );
};

export default AsicCell;
