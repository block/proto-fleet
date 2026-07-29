import { Dispatch, SetStateAction } from "react";
import clsx from "clsx";

import { getAsicsRows } from "../utility";
import AsicButton from "./AsicButton";
import { AsicData, useIsProtoContainer } from "@/protoOS/store";
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
  const isProtoContainer = useIsProtoContainer();

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
            {/* Proto container modules use a responsive grid: cells flex to
                fill the available width when the viewport allows, and hold a
                56px floor (min-w-14) so the row scrolls horizontally when it
                can't. Rigs keep the original full-width flex rows. */}
            <div className={isProtoContainer ? "flex w-full flex-col gap-1" : "w-full -space-y-[2px]"}>
              {getAsicsRows(asics).map((row) => (
                <div className={clsx("flex", isProtoContainer ? "gap-1" : "gap-1.5")} key={`asic-${row}`}>
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
