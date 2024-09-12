import { Dispatch, SetStateAction, useEffect, useState } from "react";

import { useHashboardStats } from "api";
import { AsicStats, HashrateResponseHashratedata } from "apiTypes";

import { Granularity } from "../types";
import { sortAsics } from "../utility";
import AsicTable from "./AsicTable";

interface AsicTableWrapperProps {
  duration: HashrateResponseHashratedata["duration"];
  granularity: Granularity;
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
