import { useState } from "react";
import clsx from "clsx";

import { rowTitles } from "./constants";
import Filters from "./Filters";
import rowConfig from "./rowConfig";
import { type Miner, type RowName } from "./types";
import { Ellipsis } from "@/shared/assets/icons";
import Checkbox from "@/shared/components/Checkbox";

type MinerListProps = {
  title: string;
  miners: Miner[];
};

const cellClassList = "py-4 text-left";
const thClassList = cellClassList + " text-emphasis-300";
const tdClassList = cellClassList + " text-300";
const rowClassList = "border-b border-border-5";

// TODO: move this to state when we
// implement row customization
const activeRows: RowName[] = [
  "name",
  "macAddress",
  "status",
  "hashrate",
  "efficiency",
  "powerUsage",
  "temperature",
];

const MinerList = ({ title, miners = [] }: MinerListProps) => {
  const [selectedMiners, setSelectedMiners] = useState<string[]>([]);
  const [filteredMiners, setFilteredMiners] = useState<Miner[]>(miners);

  const handleSelectAll = (checked: boolean) => {
    if (checked) {
      setSelectedMiners(miners.map((miner) => miner.macAddress));
    } else {
      setSelectedMiners([]);
    }
  };

  const handleSelectMiner = (macAddress: string, checked: boolean) => {
    setSelectedMiners((prev) => {
      if (checked && !prev.includes(macAddress)) {
        return [...prev, macAddress];
      } else if (!checked) {
        return prev.filter((addr) => addr !== macAddress);
      }
      return prev;
    });
  };

  const allSelected =
    miners.length > 0 && selectedMiners.length === miners.length;

  return (
    <div>
      <h2 className="text-heading-300">{title}</h2>
      <Filters miners={miners} setFilteredMiners={setFilteredMiners} />
      <div className="overflow-x-auto">
        <table className="min-w-full table-fixed border-collapse">
          <thead data-testid="miner-list-header">
            <tr className={rowClassList}>
              <th className={thClassList}>
                <div className="w-12 truncate overflow-hidden">
                  <Checkbox
                    checked={allSelected}
                    onChange={(e) => handleSelectAll(e.target.checked)}
                  />
                </div>
              </th>

              {activeRows.map((row, idx) => (
                <th className={thClassList} key={idx}>
                  <div
                    className={clsx(
                      "truncate overflow-hidden",
                      rowConfig[row]?.width,
                    )}
                  >
                    {rowTitles[row as keyof typeof rowTitles]}
                  </div>
                </th>
              ))}

              <th className={thClassList}>
                <div className="w-12 truncate overflow-hidden">
                  <button className="align-middle text-text-primary-30 hover:cursor-pointer hover:text-text-primary-50">
                    <Ellipsis />
                  </button>
                </div>
              </th>
            </tr>
          </thead>
          <tbody data-testid="miner-list-body">
            {filteredMiners.map((miner, i) => (
              <tr
                key={i}
                className={clsx(rowClassList, "hover:cursor-pointer")}
              >
                <td className={tdClassList}>
                  <div className="w-12 truncate overflow-hidden">
                    <Checkbox
                      checked={selectedMiners.includes(miner.macAddress)}
                      onChange={(e) =>
                        handleSelectMiner(miner.macAddress, e.target.checked)
                      }
                    />
                  </div>
                </td>

                {activeRows.map((row, j) => (
                  <td className={tdClassList} key={j}>
                    <div
                      className={clsx(
                        "truncate overflow-hidden",
                        rowConfig[row]?.width,
                      )}
                    >
                      {rowConfig[row]?.component
                        ? rowConfig[row].component(miner, selectedMiners)
                        : miner[row].toString()}
                    </div>
                  </td>
                ))}

                <td className={tdClassList}>
                  <div className="w-12 truncate overflow-hidden"></div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
};

export default MinerList;
