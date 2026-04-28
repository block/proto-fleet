import { useState } from "react";
import HashboardSelectorComponent from "./HashboardSelector";
import useMinerStore from "@/protoOS/store/useMinerStore";

// Mock hashboard data for the story
const mockHashboards = [
  { serial: "hb-001", slot: 1 },
  { serial: "hb-002", slot: 2 },
  { serial: "hb-003", slot: 3 },
];

// Initialize store with mock data once
mockHashboards.forEach((hb) => {
  useMinerStore.getState().hardware.addHashboard({
    serial: hb.serial,
    slot: hb.slot,
  });
});

export const HashboardSelector = () => {
  const [activeChartLines, setActiveChartLines] = useState<string[]>(["miner", "hb-001"]);

  const chartLines = mockHashboards.map((hb) => hb.serial);

  return (
    <div className="flex min-h-[200px] items-center justify-center">
      <HashboardSelectorComponent
        chartLines={chartLines}
        setActiveChartLines={setActiveChartLines}
        activeChartLines={activeChartLines}
        aggregateKey="miner"
      />
    </div>
  );
};

export const WithNoSelection = () => {
  const [activeChartLines, setActiveChartLines] = useState<string[]>([]);

  const chartLines = mockHashboards.map((hb) => hb.serial);

  return (
    <div className="flex min-h-[200px] items-center justify-center">
      <HashboardSelectorComponent
        chartLines={chartLines}
        setActiveChartLines={setActiveChartLines}
        activeChartLines={activeChartLines}
        aggregateKey="miner"
      />
    </div>
  );
};

export const WithAllSelected = () => {
  const [activeChartLines, setActiveChartLines] = useState<string[]>(["miner", "hb-001", "hb-002", "hb-003"]);

  const chartLines = mockHashboards.map((hb) => hb.serial);

  return (
    <div className="flex min-h-[200px] items-center justify-center">
      <HashboardSelectorComponent
        chartLines={chartLines}
        setActiveChartLines={setActiveChartLines}
        activeChartLines={activeChartLines}
        aggregateKey="miner"
      />
    </div>
  );
};

export default {
  title: "Proto OS/HashboardSelector",
  component: HashboardSelectorComponent,
};
