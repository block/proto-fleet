import { minerCols } from "./constants";
import Hashrate from "./Hashrate";
import MinerStatus from "./MinerStatus";
import { type Miner } from "./types";
import { type ColConfig } from "@/shared/components/List/types";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { statuses } from "@/shared/components/StatusCircle/constants";

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
          status={
            item.status ?? {
              hashboard: statuses.inactive,
              cb: statuses.inactive,
              fans: statuses.inactive,
              asic: statuses.inactive,
            }
          }
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
    component: (item: Miner) => {
      return (
        <>
          {item.efficiency ? (
            <>{item.efficiency} J/TH</>
          ) : (
            <SkeletonBar className="w-full pr-10" />
          )}
        </>
      );
    },
    width: "w-38",
  },
  [minerCols.powerUsage]: {
    component: (item: Miner) => {
      return (
        <>
          {item.powerUsage ? (
            <>{item.powerUsage} kW</>
          ) : (
            <SkeletonBar className="w-full pr-10" />
          )}
        </>
      );
    },
    width: "w-38",
  },
  [minerCols.temperature]: {
    component: (item: Miner) => {
      return (
        <>
          {item.temperature ? (
            <>{item.temperature} °C</>
          ) : (
            <SkeletonBar className="w-full pr-10" />
          )}
        </>
      );
    },
    width: "w-38",
  },
};

export default minerColConfig;
