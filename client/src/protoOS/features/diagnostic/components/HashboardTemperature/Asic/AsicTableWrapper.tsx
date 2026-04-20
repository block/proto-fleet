import { Dispatch, SetStateAction, useMemo } from "react";

import { sortAsics } from "../utility";
import AsicTable from "./AsicTable";
import { useMinerHashboardAsics } from "@/protoOS/store";

interface AsicTableWrapperProps {
  hashboardSerialNumber: string;
  showPopover: string | undefined;
  setShowPopover: Dispatch<SetStateAction<string | undefined>>;
}

const AsicTableWrapper = ({ hashboardSerialNumber, showPopover, setShowPopover }: AsicTableWrapperProps) => {
  const asics = useMinerHashboardAsics(hashboardSerialNumber);

  const sortedAsics = useMemo(() => {
    return sortAsics(asics);
  }, [asics]);

  return (
    <AsicTable
      asics={sortedAsics}
      hashboardSerialNumber={hashboardSerialNumber}
      pending={asics.length === 0}
      showPopover={showPopover}
      setShowPopover={setShowPopover}
    />
  );
};

export default AsicTableWrapper;
