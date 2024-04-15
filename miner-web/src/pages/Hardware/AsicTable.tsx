import { useEffect, useState } from "react";
import clsx from "clsx";

import { useHashboardStats } from "api";
import { AsicStats } from "apiTypes";

import { dangerTemp, getAsics, warningTemp } from "./constants";
import { getAsicsRows, getRowLabel, sortAsics } from "./utility";

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
    <div className="flex mt-6">
      <div className="space-y-2 mt-[34px] mr-2">
        {/* Row label */}
        {asics
          .filter((asic) => asic.col === 0)
          .map((asic) => (
            <div
              className="bg-surface-5 text-mono-text-50 text-text-primary/90 px-2 py-1 rounded-lg border border-border-primary/5 text-center flex items-center h-[42px]"
              key={`asic-header-${asic.row}`}
            >
              {getRowLabel(asic.row || 0)}
            </div>
          ))}
      </div>
      <div className="w-full space-y-2">
        <div className="flex space-x-2">
          {/* Column label */}
          {asics
            .filter((asic) => asic.row === 0)
            .map((asic) => (
              <div
                className="bg-surface-5 text-mono-text-50 text-text-primary/90 px-2 py-1 rounded-lg border border-border-primary/5 basis-0 grow text-center"
                key={`asic-header-${asic.col}`}
              >
                {(asic.col || 0) + 1}
              </div>
            ))}
        </div>
        {/* Individual ASICs */}
        {getAsicsRows(asics).map((row) => (
          <div className="flex space-x-2" key={`asic-${row}`}>
            {asics
              .filter((asic) => asic.row === row)
              .map((asic) => (
                <div
                  className={clsx(
                    "text-mono-text-50 text-text-primary/90 px-1 py-3 rounded-lg border border-border-primary/5 basis-0 grow text-center",
                    {
                      "bg-intent-warning-fill/50":
                        (asic.temp_c || 0) >= warningTemp &&
                        (asic.temp_c || 0) < dangerTemp,
                      "bg-intent-warning-fill":
                        (asic.temp_c || 0) >= dangerTemp,
                    }
                  )}
                  key={`asic-${asic.row}-${asic.col}`}
                >
                  {asic.temp_c}º
                </div>
              ))}
          </div>
        ))}
      </div>
    </div>
  );
};

export default AsicTable;
