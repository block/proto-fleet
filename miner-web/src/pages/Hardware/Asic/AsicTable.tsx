import { useEffect, useState } from "react";

import { useHashboardStats } from "api";
import { AsicStats } from "apiTypes";

import { getAsics } from "../constants";
import { getAsicsRows, getRowLabel, sortAsics } from "../utility";
import AsicButton from "./AsicButton";

interface AsicTableProps {
  hashboardSerialNumber: string;
}

const AsicTable = ({ hashboardSerialNumber }: AsicTableProps) => {
  const { data } = useHashboardStats(hashboardSerialNumber);
  const [asics, setAsics] = useState<AsicStats[]>([]);

  useEffect(() => {
    if (data?.asics && !asics.length) {
      // TODO: remove else when API returns real data
      if (data.asics.length > 1) {
        setAsics(sortAsics(data.asics));
      } else {
        setAsics(sortAsics(getAsics()));
      }
    }
  }, [asics, data]);

  return (
    <div className="mt-6 relative">
      <div className="flex phone:overflow-x-scroll">
        <div className="space-y-2 mt-[34px] mr-[3px]">
          {/* Row label */}
          {asics
            .filter((asic) => asic.column === 0)
            .map((asic) => (
              <div
                className="bg-surface-5 text-mono-text-50 text-text-primary/90 px-2 py-1 rounded-lg border border-border-primary/5 text-center flex items-center h-[42px]"
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
                  className="bg-surface-5 text-mono-text-50 text-text-primary/90 px-2 py-1 rounded-lg border border-border-primary/5 basis-0 grow text-center"
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
                    key={`asic-${asic.row}-${asic.column}`}
                  />
                ))}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};

export default AsicTable;
