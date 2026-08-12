import { Dispatch, SetStateAction, useMemo } from "react";
import clsx from "clsx";

import { groupAsicsByRow } from "../utility";
import AsicButton from "./AsicButton";
import { useHashboardLayout } from "@/protoOS/features/diagnostic/hashboardLayout";
import { getAsicColor, useAsicPalette } from "@/protoOS/features/kpis/hooks";
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
  const layout = useHashboardLayout();
  // Resolved once for the whole grid; see useAsicPalette.
  const palette = useAsicPalette();

  const rows = useMemo(() => groupAsicsByRow(asics), [asics]);

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
            <div className={layout.grid.frame}>
              {rows.map(({ row, asics: rowAsics }) => (
                <div className={clsx("flex", layout.grid.row)} key={`asic-${row}`}>
                  {rowAsics.map((asic) => (
                    <PopoverProvider key={`asic-${asic.row}-${asic.column}`}>
                      <AsicButton
                        asic={asic}
                        backgroundColor={getAsicColor(palette, asic.temperature?.latest?.value)}
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
