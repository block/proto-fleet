import { minerCols, type MinerColumn } from "./constants";
import MinerEfficiency from "./MinerEfficiency";
import MinerFirmware from "./MinerFirmware";
import MinerHashrate from "./MinerHashrate";
import MinerIpAddress from "./MinerIpAddress";
import MinerIssuesCell from "./MinerIssuesCell";
import MinerMacAddress from "./MinerMacAddress";
import MinerModel from "./MinerModel";
import MinerName from "./MinerName";
import MinerPowerUsage from "./MinerPowerUsage";
import MinerStatusCell from "./MinerStatusCell";
import MinerTemperature from "./MinerTemperature";
import { type DeviceListItem } from "./types";
import { type ColConfig } from "@/shared/components/List/types";

type CreateMinerColConfigParams = {
  onOpenStatusFlow: (deviceIdentifier: string) => void;
};

const createMinerColConfig = ({
  onOpenStatusFlow,
}: CreateMinerColConfigParams): ColConfig<DeviceListItem, string, MinerColumn> => ({
  [minerCols.name]: {
    component: (device: DeviceListItem) => (
      <MinerName deviceIdentifier={device.deviceIdentifier} onOpenStatusFlow={onOpenStatusFlow} />
    ),
    width: "min-w-20",
  },
  [minerCols.model]: {
    component: (device: DeviceListItem) => <MinerModel deviceIdentifier={device.deviceIdentifier} />,
    width: "min-w-28",
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
      <MinerStatusCell
        deviceIdentifier={device.deviceIdentifier}
        selectedItems={selectedItems}
        onOpenStatusFlow={onOpenStatusFlow}
      />
    ),
    width: "min-w-48",
  },
  [minerCols.issues]: {
    component: (device: DeviceListItem) => (
      <MinerIssuesCell deviceIdentifier={device.deviceIdentifier} onOpenStatusFlow={onOpenStatusFlow} />
    ),
    width: "min-w-48",
  },
  [minerCols.hashrate]: {
    component: (device: DeviceListItem) => <MinerHashrate deviceIdentifier={device.deviceIdentifier} />,
    width: "min-w-20",
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
  [minerCols.firmware]: {
    component: (device: DeviceListItem) => <MinerFirmware deviceIdentifier={device.deviceIdentifier} />,
    width: "min-w-28",
  },
});

export default createMinerColConfig;
