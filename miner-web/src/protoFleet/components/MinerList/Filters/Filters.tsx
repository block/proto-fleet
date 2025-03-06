import { useEffect, useMemo, useState } from "react";

// import DropDown from "@/shared/components/DropDown";
import { Miner } from "../types";
import FilterItem from "./FilterItem";

export type FilterType = "all" | "hashing" | "broken" | "offline" | "asleep";

type FilterProps = {
  setFilteredMiners: (miners: Miner[]) => void;
  miners: Miner[];
};

const Filters = ({ miners, setFilteredMiners }: FilterProps) => {
  const [activeFilter, setActiveFilter] = useState<FilterType>("all");

  const hashingCount = useMemo(
    () => miners.filter((m) => m.status.hashing === true).length,
    [miners],
  );
  const brokenCount = useMemo(
    () => miners.filter((m) => m.status.broken === true).length,
    [miners],
  );
  const offlineCount = useMemo(
    () => miners.filter((m) => m.status.offline === true).length,
    [miners],
  );
  const asleepCount = useMemo(
    () => miners.filter((m) => m.status.asleep === true).length,
    [miners],
  );

  useEffect(() => {
    setFilteredMiners(
      miners.filter(
        (miner) =>
          miner.status[activeFilter as keyof Miner["status"]] === true ||
          activeFilter === "all",
      ),
    );
  }, [activeFilter, miners, setFilteredMiners]);

  return (
    <div className="flex flex-row gap-4 py-4">
      <FilterItem
        title="All Miners"
        filter="all"
        activeFilter={activeFilter}
        setActiveFilter={setActiveFilter}
      />

      <FilterItem
        status="normal"
        count={hashingCount}
        title="Hashing"
        filter="hashing"
        activeFilter={activeFilter}
        setActiveFilter={setActiveFilter}
      />

      <FilterItem
        status="error"
        count={brokenCount}
        title="Broken"
        filter="broken"
        activeFilter={activeFilter}
        setActiveFilter={setActiveFilter}
      />

      <FilterItem
        status="warning"
        count={offlineCount}
        title="Offline"
        filter="offline"
        activeFilter={activeFilter}
        setActiveFilter={setActiveFilter}
      />

      <FilterItem
        status="inactive"
        count={asleepCount}
        title="Asleep"
        filter="asleep"
        activeFilter={activeFilter}
        setActiveFilter={setActiveFilter}
      />

      {/* <DropDown 
        title="Model"
        options={["Proto R1", "Proto R2"]}
        selectedValue=""
        onChange={(value) => console.log(value)}
      />

      <DropDown 
        title="Rack"
        options={["Rack 1", "Rack 2", "Rack 3"]}
        selectedValue=""
        onChange={(value) => console.log(value)}
      /> */}
    </div>
  );
};

export default Filters;
