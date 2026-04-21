import { Dispatch, SetStateAction } from "react";

import { getAsicsRows } from "../utility";
import AsicButton from "./AsicButton";
import { AsicData } from "@/protoOS/store";
import { PopoverProvider } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface AsicTableProps {
  asics: AsicData[];
  hashboardSerialNumber: string;
  pending: boolean;
  showPopover: string | undefined;
  setShowPopover: Dispatch<SetStateAction<string | undefined>>;
}

const AsicTable = ({ asics, hashboardSerialNumber, pending, showPopover, setShowPopover }: AsicTableProps) => {
  return (
    <div className="relative mt-6 h-full">
      <div className="flex h-full">
        {pending && !asics.length ? (
          <div className="flex h-full w-full max-w-[calc(100vw-theme(spacing.3))] items-center justify-center">
            <div className="py-10">
              <ProgressCircular indeterminate />
            </div>
          </div>
        ) : (
          <>
            <div className="w-full -space-y-[2px]">
              {/* Individual ASICs */}
              {getAsicsRows(asics).map((row) => (
                <div className="flex gap-1.5" key={`asic-${row}`}>
                  {asics
                    .filter((asic) => asic.row === row)
                    .map((asic) => (
                      <PopoverProvider key={`asic-${asic.row}-${asic.column}`}>
                        <AsicButton
                          asic={asic}
                          hashboardSerial={hashboardSerialNumber}
                          showPopover={showPopover}
                          setShowPopover={setShowPopover}
                          totalAsicCount={asics.length}
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
