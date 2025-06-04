import { minerCols } from "./constants";
import Hashrate from "./Hashrate";
import MinerEfficiency from "./MinerEfficiency";
import MinerMacAddress from "./MinerMacAddress";
import MinerName from "./MinerName";
import MinerPowerUsage from "./MinerPowerUsage";
import MinerStatus from "./MinerStatus";
import MinerTemperature from "./MinerTemperature";
import { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { type ColConfig } from "@/shared/components/List/types";

// Import all the separate components

type MinerItem = Pick<MinerStateSnapshot, "deviceIdentifier">;

const minerColConfig: ColConfig<MinerItem, string> = {
  [minerCols.name]: {
    component: (item: MinerItem) => (
      <MinerName deviceIdentifier={item.deviceIdentifier} />
    ),
    width: "w-24",
  },
  [minerCols.macAddress]: {
    component: (item: MinerItem) => (
      <MinerMacAddress deviceIdentifier={item.deviceIdentifier} />
    ),
    width: "w-38",
  },
  [minerCols.status]: {
    component: (item: MinerItem, selectedItems: string[]) => (
      <MinerStatus
        deviceIdentifier={item.deviceIdentifier}
        selectedItems={selectedItems}
      />
    ),
    width: "w-17",
  },
  [minerCols.hashrate]: {
    component: (item: MinerItem) => (
      <Hashrate deviceIdentifier={item.deviceIdentifier} />
    ),
    width: "w-41",
  },
  [minerCols.efficiency]: {
    component: (item: MinerItem) => (
      <MinerEfficiency deviceIdentifier={item.deviceIdentifier} />
    ),
    width: "w-38",
  },
  [minerCols.powerUsage]: {
    component: (item: MinerItem) => (
      <MinerPowerUsage deviceIdentifier={item.deviceIdentifier} />
    ),
    width: "w-38",
  },
  [minerCols.temperature]: {
    component: (item: MinerItem) => (
      <MinerTemperature deviceIdentifier={item.deviceIdentifier} />
    ),
    width: "w-38",
  },
};

export default minerColConfig;
