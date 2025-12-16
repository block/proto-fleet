import { minerCols, type MinerColumn } from "./constants";
import Hashrate from "./Hashrate";
import MinerEfficiency from "./MinerEfficiency";
import MinerIpAddress from "./MinerIpAddress";
import MinerMacAddress from "./MinerMacAddress";
import MinerName from "./MinerName";
import MinerPowerUsage from "./MinerPowerUsage";
import MinerStatusCell from "./MinerStatusCell";
import MinerTemperature from "./MinerTemperature";
import { type DeviceListItem } from "./types";
import { type ColConfig } from "@/shared/components/List/types";

const minerColConfig: ColConfig<DeviceListItem, string, MinerColumn> = {
  [minerCols.name]: {
    component: (device: DeviceListItem) => <MinerName deviceIdentifier={device.deviceIdentifier} />,
    width: "min-w-20",
  },
  [minerCols.macAddress]: {
    component: (device: DeviceListItem) => <MinerMacAddress deviceIdentifier={device.deviceIdentifier} />,
    width: "min-w-36",
  },
  [minerCols.ipAddress]: {
    component: (device: DeviceListItem) => <MinerIpAddress deviceIdentifier={device.deviceIdentifier} />,
    width: "min-w-24",
  },
  [minerCols.status]: {
    component: (device: DeviceListItem, selectedItems: string[]) => (
      <MinerStatusCell deviceIdentifier={device.deviceIdentifier} selectedItems={selectedItems} />
    ),
    width: "min-w-74",
  },
  [minerCols.hashrate]: {
    component: (device: DeviceListItem) => <Hashrate deviceIdentifier={device.deviceIdentifier} />,
    width: "min-w-38",
  },
  [minerCols.efficiency]: {
    component: (device: DeviceListItem) => <MinerEfficiency deviceIdentifier={device.deviceIdentifier} />,
    width: "min-w-20",
  },
  [minerCols.powerUsage]: {
    component: (device: DeviceListItem) => <MinerPowerUsage deviceIdentifier={device.deviceIdentifier} />,
    width: "min-w-20",
  },
  [minerCols.temperature]: {
    component: (device: DeviceListItem) => <MinerTemperature deviceIdentifier={device.deviceIdentifier} />,
    width: "min-w-20",
  },
};

export default minerColConfig;
