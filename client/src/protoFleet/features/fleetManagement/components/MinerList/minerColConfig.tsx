import { minerCols } from "./constants";
import Hashrate from "./Hashrate";
import MinerStatus from "./MinerStatus";
import {
  ComponentStatus,
  type MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { type ColConfig } from "@/shared/components/List/types";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { getDisplayValue } from "@/shared/utils/stringUtils";

type MinerKeyValueType = MinerStateSnapshot["macAddress"];
const minerColConfig: ColConfig<MinerStateSnapshot, MinerKeyValueType> = {
  [minerCols.name]: {
    width: "w-24",
  },
  [minerCols.macAddress]: {
    width: "w-38",
  },
  [minerCols.status]: {
    component: (
      item: MinerStateSnapshot,
      selectedItems: MinerKeyValueType[],
    ) => {
      return (
        <MinerStatus
          isSelected={selectedItems.includes(item.macAddress)}
          status={
            item.status ?? {
              $typeName: "fleetmanagement.v1.MinerComponentStatus",
              hashBoards: ComponentStatus.UNSPECIFIED,
              controlBoard: ComponentStatus.UNSPECIFIED,
              fans: ComponentStatus.UNSPECIFIED,
              psu: ComponentStatus.UNSPECIFIED,
            }
          }
        />
      );
    },
    width: "w-17",
  },
  [minerCols.hashrate]: {
    component: (
      item: MinerStateSnapshot,
      selectedItems: MinerKeyValueType[],
    ) => {
      void selectedItems;
      return <Hashrate hashrate={item.hashrate} />;
    },
    width: "w-41",
  },
  [minerCols.efficiency]: {
    component: (item: MinerStateSnapshot) => {
      return (
        <>
          {item.efficiency ? (
            <>{getDisplayValue(item.efficiency[0].value)} J/TH</>
          ) : (
            <SkeletonBar className="w-full pr-10" />
          )}
        </>
      );
    },
    width: "w-38",
  },
  [minerCols.powerUsage]: {
    component: (item: MinerStateSnapshot) => {
      return (
        <>
          {item.powerUsage ? (
            <>{getDisplayValue(item.powerUsage[0].value)} kW</>
          ) : (
            <SkeletonBar className="w-full pr-10" />
          )}
        </>
      );
    },
    width: "w-38",
  },
  [minerCols.temperature]: {
    component: (item: MinerStateSnapshot) => {
      return (
        <>
          {item.temperature ? (
            <>{getDisplayValue(item.temperature[0].value)} °C</>
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
