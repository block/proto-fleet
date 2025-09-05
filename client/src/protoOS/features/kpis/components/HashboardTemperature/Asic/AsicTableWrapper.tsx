import { Dispatch, SetStateAction, useMemo } from "react";

import useHashboardAsicStore from "@/protoOS/store/useHashboardAsicStore";
import AsicTable from "./AsicTable";
import { GetAsicHashrateParams } from "@/protoOS/api/types";
import { type Duration } from "@/shared/components/DurationSelector";
import { sortAsics } from "@/protoOS/features/kpis/components/Temperature/utility";
import { useHashboardStats } from "@/protoOS/api";

interface AsicTableWrapperProps {
  duration: Duration;
  granularity: GetAsicHashrateParams["granularity"];
  hashboardSerialNumber: string;
  showPopover: string | undefined;
  setShowPopover: Dispatch<SetStateAction<string | undefined>>;
}

const AsicTableWrapper = ({
  duration,
  granularity,
  hashboardSerialNumber,
  showPopover,
  setShowPopover,
}: AsicTableWrapperProps) => {
  const { pending } = useHashboardStats({
    hashboardSerialNumber,
    poll: true,
  });

  const hashboard = useHashboardAsicStore((state) =>
    state.hashboards.get(hashboardSerialNumber),
  );

  const asics = useMemo(() => {
    if (!hashboard) return [];
    return sortAsics(Array.from(hashboard.asics.values()));
  }, [hashboard?.asics]);

  return (
    <AsicTable
      asics={asics}
      duration={duration}
      granularity={granularity}
      hashboardSerialNumber={hashboardSerialNumber}
      pending={pending}
      showPopover={showPopover}
      setShowPopover={setShowPopover}
    />
  );
};

export default AsicTableWrapper;
