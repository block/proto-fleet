import { useAsicColor } from "../../../hooks";
import type { AsicStats } from "@/protoOS/api/types";

const AsicCell = ({ asic }: { asic: AsicStats }) => {
  const backgroundColor = useAsicColor(asic);

  return (
    <div
      style={{ backgroundColor }}
      className="relative h-4 grow basis-0 rounded-xl border-1 border-core-primary-5"
    />
  );
};

export default AsicCell;
