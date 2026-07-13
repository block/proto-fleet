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
import {
  getMinerBuildingLabel,
  getMinerCohortLabel,
  getMinerRackLabel,
  getMinerSiteLabel,
} from "@/protoFleet/features/fleetManagement/utils/minerPlacement";
import { type ColConfig } from "@/shared/components/List/types";
import SkeletonBar from "@/shared/components/SkeletonBar";

type CreateMinerColConfigParams = {
  onOpenStatusFlow: (deviceIdentifier: string) => void;
  availableGroups: DeviceSet[];
  errorsLoaded: boolean;
  /** Ref to avoid recreating the column config on every miners change. Read at render time. */
  minersRef: MutableRefObject<Record<string, MinerStateSnapshot>>;
  /** Ref to avoid recreating the column config on every callback change. Read at render time. */
  onRefetchMinersRef: MutableRefObject<(() => void) | undefined>;
  /** Ref to avoid recreating the column config on every callback change. Read at render time. */
  onRefreshMinersCompleteRef: MutableRefObject<(() => void) | undefined>;
  /** Ref to avoid recreating the column config on every callback change. Read at render time. */
  onWorkerNameUpdatedRef: MutableRefObject<((deviceIdentifier: string, workerName: string) => void) | undefined>;
  /** Ref to avoid recreating the column config on every callback change. Read at render time. */
  onMergeMinersRef: MutableRefObject<((snapshots: MinerStateSnapshot[]) => void) | undefined>;
  /** Ref to avoid recreating the column config on every callback change. Read at render time. */
  onMinerRefreshStateChangeRef: MutableRefObject<
    ((deviceIdentifier: string, isRefreshing: boolean) => void) | undefined
  >;
};

const renderPlacementLabel = (label: string) => (
  <span className="block truncate" title={label || undefined}>
    {label}
  </span>
);

const createMinerColConfig = ({
  onOpenStatusFlow,
  availableGroups,
  errorsLoaded,
  minersRef,
  onRefetchMinersRef,
  onRefreshMinersCompleteRef,
  onWorkerNameUpdatedRef,
  onMergeMinersRef,
  onMinerRefreshStateChangeRef,
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
          onRefreshMinersComplete={onRefreshMinersCompleteRef.current}
          onWorkerNameUpdated={onWorkerNameUpdatedRef.current}
          onMergeMiners={onMergeMinersRef.current}
          onMinerRefreshStateChange={onMinerRefreshStateChangeRef.current}
        />
      );
    },
    width: "w-[208px]",
  },
  [minerCols.workerName]: {
    component: (device: DeviceListItem) => <MinerWorkerName miner={device.miner} />,
    width: "w-[176px]",
    allowOverflow: true,
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
      <MinerStatusCell
        device={device}
        errorsLoaded={errorsLoaded}
        onOpenStatusFlow={onOpenStatusFlow}
        isRefreshing={device.isRefreshing}
      />
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
    component: (device: DeviceListItem) =>
      device.isRefreshing ? <SkeletonBar className="w-full pr-10" /> : <MinerHashrate miner={device.miner} />,
    width: "w-[80px]",
  },
  [minerCols.efficiency]: {
    component: (device: DeviceListItem) =>
      device.isRefreshing ? <SkeletonBar className="w-full pr-10" /> : <MinerEfficiency miner={device.miner} />,
    width: "w-[80px]",
  },
  [minerCols.powerUsage]: {
    component: (device: DeviceListItem) =>
      device.isRefreshing ? <SkeletonBar className="w-full pr-10" /> : <MinerPowerUsage miner={device.miner} />,
    width: "w-[80px]",
  },
  [minerCols.temperature]: {
    component: (device: DeviceListItem) =>
      device.isRefreshing ? <SkeletonBar className="w-full pr-10" /> : <MinerTemperature miner={device.miner} />,
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
  [minerCols.cohort]: {
    component: (device: DeviceListItem) => renderPlacementLabel(getMinerCohortLabel(device.miner)),
    width: "w-[160px]",
  },
  [minerCols.site]: {
    component: (device: DeviceListItem) => renderPlacementLabel(getMinerSiteLabel(device.miner)),
    width: "w-[160px]",
  },
  [minerCols.building]: {
    component: (device: DeviceListItem) => renderPlacementLabel(getMinerBuildingLabel(device.miner)),
    width: "w-[160px]",
  },
  [minerCols.rack]: {
    component: (device: DeviceListItem) => renderPlacementLabel(getMinerRackLabel(device.miner)),
    width: "w-[160px]",
  },
});

export default createMinerColConfig;
