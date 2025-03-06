import { Dispatch, SetStateAction } from "react";

import { getAsicsRows } from "../utility";
import AsicButton from "./AsicButton";
import { AsicStats, GetAsicHashrateParams } from "@/protoOS/api/types";
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
    <div className="mt-6 relative h-full">
      <div className="flex phone:overflow-x-scroll h-full">
        {pending && !asics.length ? (
          <div className="flex w-full h-full items-center justify-center">
            <Spinner />
          </div>
        ) : (
          <>
            <div className="space-y-2 mt-[34px] mr-[3px]">
              {/* Row label */}
              {asics
                .filter((asic) => asic.column === 0)
                .map((asic) => (
                  <div
                    className="bg-core-primary-5 font-mono text-mono-text-50 text-text-primary px-2 py-1 rounded-lg border border-border-5 text-center flex items-center h-[42px]"
                    key={`asic-header-${asic.row}`}
                  >
                    {getRowLabel(asic.row || 0)}
                  </div>
                ))}
            </div>
            <div className="w-full -space-y-[2px]">
              <div className="flex space-x-2 mx-[5px] mb-[5px]">
                {/* Column label */}
                {asics
                  .filter((asic) => asic.row === 0)
                  .map((asic) => (
                    <div
                      className="bg-core-primary-5 font-mono text-mono-text-50 text-text-primary px-2 py-1 rounded-lg border border-border-5 basis-0 grow text-center"
                      key={`asic-header-${asic.column}`}
                    >
                      {(asic.column || 0) + 1}
                    </div>
                  ))}
              </div>
              {/* Individual ASICs */}
              {getAsicsRows(asics).map((row) => (
                <div className="flex -space-x-[2px]" key={`asic-${row}`}>
                  {asics
                    .filter((asic) => asic.row === row)
                    .map((asic) => (
                      <AsicButton
                        asic={asic}
                        duration={duration}
                        granularity={granularity}
                        hashboardSerial={hashboardSerialNumber}
                        showPopover={showPopover}
                        setShowPopover={setShowPopover}
                        key={`asic-${asic.row}-${asic.column}`}
                      />
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
