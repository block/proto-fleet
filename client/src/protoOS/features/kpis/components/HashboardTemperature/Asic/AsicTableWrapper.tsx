import { Dispatch, SetStateAction, useMemo } from "react";

import AsicTable from "./AsicTable";
import { useHashboardStats } from "@/protoOS/api";
import { GetAsicHashrateParams } from "@/protoOS/api/types";
import { sortAsics } from "@/protoOS/features/kpis/components/Temperature/utility";
import useHashboardAsicStore from "@/protoOS/store/useHashboardAsicStore";
import { type Duration } from "@/shared/components/DurationSelector";

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
  }, [hashboard]);

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
