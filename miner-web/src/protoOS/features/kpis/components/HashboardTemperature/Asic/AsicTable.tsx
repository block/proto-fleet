import { Dispatch, SetStateAction } from "react";

import { getAsicsRows } from "../../Temperature/utility";
import AsicButton from "./AsicButton";
import { AsicStats, GetAsicHashrateParams } from "@/protoOS/api/types";
import { PopoverProvider } from "@/shared/components/Popover";
import Spinner from "@/shared/components/Spinner";

interface AsicTableProps {
  asics: AsicStats[];
  duration: GetAsicHashrateParams["duration"];
  granularity: GetAsicHashrateParams["granularity"];
  hashboardSerialNumber: string;
  pending: boolean;
  showPopover: string | undefined;
  setShowPopover: Dispatch<SetStateAction<string | undefined>>;
}

const AsicTable = ({
  asics,
  duration,
  granularity,
  hashboardSerialNumber,
  pending,
  showPopover,
  setShowPopover,
}: AsicTableProps) => {
  return (
    <div className="relative mt-6 h-full">
      <div className="flex h-full">
        {pending && !asics.length ? (
          <div className="flex h-full w-full max-w-[calc(100vw-theme(spacing.3))] items-center justify-center">
            <div className="py-10">
              <Spinner />
            </div>
          </div>
        ) : (
          <>
            <div className="w-full -space-y-[2px]">
              {/* Individual ASICs */}
              {getAsicsRows(asics).map((row) => (
                <div className="-mr-[4px] flex" key={`asic-${row}`}>
                  {asics
                    .filter((asic) => asic.row === row)
                    .map((asic) => (
                      <PopoverProvider key={`asic-${asic.row}-${asic.column}`}>
                        <AsicButton
                          asic={asic}
                          duration={duration}
                          granularity={granularity}
                          hashboardSerial={hashboardSerialNumber}
                          showPopover={showPopover}
                          setShowPopover={setShowPopover}
                        />
                      </PopoverProvider>
                    ))}
                </div>
              ))}
            </div>
          </>
        )}
      </div>
    </div>
  );
};

export default AsicTable;
