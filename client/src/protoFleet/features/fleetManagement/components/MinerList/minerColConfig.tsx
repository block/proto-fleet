import type { MutableRefObject } from "react";
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
import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { isActionLoading } from "@/protoFleet/features/fleetManagement/utils/batchStatusCheck";
import { type ColConfig } from "@/shared/components/List/types";

type CreateMinerColConfigParams = {
  onOpenStatusFlow: (deviceIdentifier: string) => void;
  availableGroups: DeviceSet[];
  errorsLoaded: boolean;
  /** Ref to avoid recreating the column config on every miners change. Read at render time. */
  minersRef: MutableRefObject<Record<string, MinerStateSnapshot>>;
  /** Ref to avoid recreating the column config on every callback change. Read at render time. */
  onRefetchMinersRef: MutableRefObject<(() => void) | undefined>;
  /** Ref to avoid recreating the column config on every callback change. Read at render time. */
  onWorkerNameUpdatedRef: MutableRefObject<((deviceIdentifier: string, workerName: string) => void) | undefined>;
};

const createMinerColConfig = ({
  onOpenStatusFlow,
  availableGroups,
  errorsLoaded,
  minersRef,
  onRefetchMinersRef,
  onWorkerNameUpdatedRef,
}: CreateMinerColConfigParams): ColConfig<DeviceListItem, string, MinerColumn> => ({
  [minerCols.name]: {
    component: (device: DeviceListItem) => {
      const loading = isActionLoading(device.activeBatches[0], device.miner.deviceStatus);

      return (
        <MinerName
          miner={device.miner}
          errors={device.errors}
          isActionLoading={loading}
          onOpenStatusFlow={onOpenStatusFlow}
          miners={minersRef.current}
          onRefetchMiners={onRefetchMinersRef.current}
          onWorkerNameUpdated={onWorkerNameUpdatedRef.current}
        />
      );
    },
    width: "w-[208px]",
  },
  [minerCols.workerName]: {
    component: (device: DeviceListItem) => <MinerWorkerName miner={device.miner} />,
    width: "w-[120px]",
  },
  [minerCols.model]: {
    component: (device: DeviceListItem) => <MinerModel miner={device.miner} />,
    width: "w-[176px]",
  },
  [minerCols.macAddress]: {
    component: (device: DeviceListItem) => <MinerMacAddress miner={device.miner} />,
    width: "w-[160px]",
  },
  [minerCols.ipAddress]: {
    component: (device: DeviceListItem) => <MinerIpAddress miner={device.miner} />,
    width: "w-24",
  },
  [minerCols.status]: {
    component: (device: DeviceListItem) => (
      <MinerStatusCell device={device} errorsLoaded={errorsLoaded} onOpenStatusFlow={onOpenStatusFlow} />
    ),
    width: "w-[200px]",
  },
  [minerCols.issues]: {
    component: (device: DeviceListItem) => (
      <MinerIssuesCell device={device} errorsLoaded={errorsLoaded} onOpenStatusFlow={onOpenStatusFlow} />
    ),
    width: "w-[200px]",
  },
  [minerCols.hashrate]: {
    component: (device: DeviceListItem) => <MinerHashrate miner={device.miner} />,
    width: "w-[80px]",
  },
  [minerCols.efficiency]: {
    component: (device: DeviceListItem) => <MinerEfficiency miner={device.miner} />,
    width: "w-[80px]",
  },
  [minerCols.powerUsage]: {
    component: (device: DeviceListItem) => <MinerPowerUsage miner={device.miner} />,
    width: "w-[80px]",
  },
  [minerCols.temperature]: {
    component: (device: DeviceListItem) => <MinerTemperature miner={device.miner} />,
    width: "w-[80px]",
  },
  [minerCols.firmware]: {
    component: (device: DeviceListItem) => <MinerFirmware miner={device.miner} />,
    width: "w-[120px]",
  },
  [minerCols.groups]: {
    component: (device: DeviceListItem) => <MinerGroups miner={device.miner} availableGroups={availableGroups} />,
    width: "w-[160px]",
  },
});

export default createMinerColConfig;
