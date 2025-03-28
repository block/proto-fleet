import { Dispatch, SetStateAction } from "react";

import { getAsicsRows } from "../../Temperature/utility";
import AsicButton from "./AsicButton";
import { AsicStats, GetAsicHashrateParams } from "@/protoOS/api/types";
import { PopoverProvider } from "@/shared/components/Popover";
import Spinner from "@/shared/components/Spinner";
import { getRowLabel } from "@/shared/utils/utility";

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
            <div className="mr-[3px] space-y-2">
              <div className="mb-[8px] h-[26px] rounded-lg border border-border-5 bg-core-primary-5"></div>

              {/* Row label */}
              {asics
                .filter((asic) => asic.column === 0)
                .map((asic) => (
                  <div
                    className="flex h-[42px] items-center rounded-lg border border-border-5 bg-core-primary-5 px-2 py-1 text-center font-mono text-mono-text-50 text-text-primary"
                    key={`asic-header-${asic.row}`}
                  >
                    {getRowLabel(asic.row || 0)}
                  </div>
                ))}
            </div>
            <div className="w-full -space-y-[2px]">
              <div className="mb-[3px] ml-[5px] flex space-x-2">
                {/* Column label */}
                {asics
                  .filter((asic) => asic.row === 0)
                  .map((asic) => (
                    <div
                      className="grow basis-0 rounded-lg border border-border-5 bg-core-primary-5 px-2 py-1 text-center font-mono text-mono-text-50 text-text-primary"
                      key={`asic-header-${asic.column}`}
                    >
                      {(asic.column || 0) + 1}
                    </div>
                  ))}
              </div>
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
