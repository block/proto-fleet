import { minerCols } from "./constants";
import Hashrate from "./Hashrate";
import MinerStatus from "./MinerStatus";
import { type Miner } from "./types";
import { type ColConfig } from "@/shared/components/List/types";

type MinerKeyValueType = Miner["macAddress"];
const minerColConfig: ColConfig<Miner, MinerKeyValueType> = {
  [minerCols.name]: {
    width: "w-24",
  },
  [minerCols.macAddress]: {
    width: "w-38",
  },
  [minerCols.status]: {
    component: (item: Miner, selectedItems: MinerKeyValueType[]) => {
      return (
        <MinerStatus
          isSelected={selectedItems.includes(item.macAddress)}
          status={item.status}
        />
      );
    },
    width: "w-17",
  },
  [minerCols.hashrate]: {
    component: (item: Miner, selectedItems: MinerKeyValueType[]) => {
      void selectedItems;
      return <Hashrate hashrate={item.hashrate} />;
    },
    width: "w-41",
  },
  [minerCols.efficiency]: {
    component: (item: Miner) => <>{item.efficiency} J/TH</>,
    width: "w-38",
  },
  [minerCols.powerUsage]: {
    component: (item: Miner) => <>{item.powerUsage} kW</>,
    width: "w-38",
  },
  [minerCols.temperature]: {
    component: (item: Miner) => <>{item.temperature}°c</>,
    width: "w-38",
  },
};

export default minerColConfig;
