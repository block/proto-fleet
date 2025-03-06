import { Dispatch, SetStateAction, useEffect, useState } from "react";

import { sortAsics } from "../utility";
import AsicTable from "./AsicTable";
import { useHashboardStats } from "@/protoOS/api";
import { AsicStats, GetAsicHashrateParams } from "@/protoOS/api/types";

interface AsicTableWrapperProps {
  duration: GetAsicHashrateParams["duration"];
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
  const { data, pending } = useHashboardStats({
    hashboardSerialNumber,
    poll: true,
  });
  const [asics, setAsics] = useState<AsicStats[]>([]);

  useEffect(() => {
    if (!pending && data?.asics?.length) {
      setAsics(sortAsics(data.asics));
    }
  }, [data, pending]);

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
