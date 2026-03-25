import { minerCols, type MinerColumn } from "./constants";
import MinerEfficiency from "./MinerEfficiency";
import MinerFirmware from "./MinerFirmware";
import MinerGroups from "./MinerGroups";
import MinerHashrate from "./MinerHashrate";
import MinerIpAddress from "./MinerIpAddress";
import MinerIssuesCell from "./MinerIssuesCell";
import MinerMacAddress from "./MinerMacAddress";
import MinerModel from "./MinerModel";
import MinerName from "./MinerName";
import MinerPowerUsage from "./MinerPowerUsage";
import MinerStatusCell from "./MinerStatusCell";
import MinerTemperature from "./MinerTemperature";
import MinerWorkerName from "./MinerWorkerName";
import { type DeviceListItem } from "./types";
import { type DeviceCollection } from "@/protoFleet/api/generated/collection/v1/collection_pb";
import { type ColConfig } from "@/shared/components/List/types";

type CreateMinerColConfigParams = {
  onOpenStatusFlow: (deviceIdentifier: string) => void;
  availableGroups: DeviceCollection[];
};

const createMinerColConfig = ({
  onOpenStatusFlow,
  availableGroups,
}: CreateMinerColConfigParams): ColConfig<DeviceListItem, string, MinerColumn> => ({
  [minerCols.name]: {
    component: (device: DeviceListItem) => (
      <MinerName deviceIdentifier={device.deviceIdentifier} onOpenStatusFlow={onOpenStatusFlow} />
    ),
    width: "w-[208px]",
  },
  [minerCols.workerName]: {
    component: (device: DeviceListItem) => <MinerWorkerName deviceIdentifier={device.deviceIdentifier} />,
    width: "w-[120px]",
  },
  [minerCols.model]: {
    component: (device: DeviceListItem) => <MinerModel deviceIdentifier={device.deviceIdentifier} />,
    width: "w-[176px]",
  },
  [minerCols.macAddress]: {
    component: (device: DeviceListItem) => <MinerMacAddress deviceIdentifier={device.deviceIdentifier} />,
    width: "w-[160px]",
  },
  [minerCols.ipAddress]: {
    component: (device: DeviceListItem) => <MinerIpAddress deviceIdentifier={device.deviceIdentifier} />,
    width: "w-24",
  },
  [minerCols.status]: {
    component: (device: DeviceListItem, selectedItems: string[]) => (
      <MinerStatusCell
        deviceIdentifier={device.deviceIdentifier}
        selectedItems={selectedItems}
        onOpenStatusFlow={onOpenStatusFlow}
      />
    ),
    width: "w-[200px]",
  },
  [minerCols.issues]: {
    component: (device: DeviceListItem) => (
      <MinerIssuesCell deviceIdentifier={device.deviceIdentifier} onOpenStatusFlow={onOpenStatusFlow} />
    ),
    width: "w-[200px]",
  },
  [minerCols.hashrate]: {
    component: (device: DeviceListItem) => <MinerHashrate deviceIdentifier={device.deviceIdentifier} />,
    width: "w-[80px]",
  },
  [minerCols.efficiency]: {
    component: (device: DeviceListItem) => <MinerEfficiency deviceIdentifier={device.deviceIdentifier} />,
    width: "w-[80px]",
  },
  [minerCols.powerUsage]: {
    component: (device: DeviceListItem) => <MinerPowerUsage deviceIdentifier={device.deviceIdentifier} />,
    width: "w-[80px]",
  },
  [minerCols.temperature]: {
    component: (device: DeviceListItem) => <MinerTemperature deviceIdentifier={device.deviceIdentifier} />,
    width: "w-[80px]",
  },
  [minerCols.firmware]: {
    component: (device: DeviceListItem) => <MinerFirmware deviceIdentifier={device.deviceIdentifier} />,
    width: "w-[120px]",
  },
  [minerCols.groups]: {
    component: (device: DeviceListItem) => (
      <MinerGroups deviceIdentifier={device.deviceIdentifier} availableGroups={availableGroups} />
    ),
    width: "w-[160px]",
  },
});

export default createMinerColConfig;
