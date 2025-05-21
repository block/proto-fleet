import {
  ComponentStatus,
  MinerComponentStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { alertTypes } from "@/protoFleet/features/fleetManagement/components/AlertsModal/constants";
import type { Alert } from "@/protoFleet/features/fleetManagement/components/AlertsModal/types";

const now = new Date();

export const alerts: Alert[] = [
  {
    minerName: "C1-M01",
    minerMacAddress: "0a:04:8a:54:fa:9f",
    minerIp: "172.27.193.225",
    minerStatus: {
      hashBoards: ComponentStatus.OK,
      psu: ComponentStatus.OK,
      fans: ComponentStatus.OK,
      controlBoard: ComponentStatus.OK,
    } as MinerComponentStatus,
    message: "Control board error",
    alertType: alertTypes.controlBoard,
    timestamp: now.getTime(),
  },
  {
    minerName: "C1-M02",
    minerMacAddress: "0b:04:8a:54:fa:9f",
    minerIp: "172.27.193.225",
    minerStatus: {
      hashBoards: ComponentStatus.WARNING,
      psu: ComponentStatus.OK,
      fans: ComponentStatus.OK,
      controlBoard: ComponentStatus.OK,
    } as MinerComponentStatus,
    message: "Fan error",
    alertType: alertTypes.fan,
    // 1 minute ago
    timestamp: now.getTime() - 1000 * 60,
  },
  {
    minerName: "C1-M03",
    minerMacAddress: "0c:04:8a:54:fa:9f",
    minerIp: "172.27.193.225",
    minerStatus: {
      hashBoards: ComponentStatus.OK,
      psu: ComponentStatus.OK,
      fans: ComponentStatus.OK,
      controlBoard: ComponentStatus.OK,
    } as MinerComponentStatus,
    message: "hashBoards error",
    alertType: alertTypes.hashboard,
    // 15 minutes ago
    timestamp: now.getTime() - 1000 * 60 * 15,
  },
  {
    minerName: "C1-M04",
    minerMacAddress: "0e:04:8a:54:fa:9f",
    minerIp: "172.27.193.225",
    minerStatus: {
      hashBoards: ComponentStatus.OK,
      psu: ComponentStatus.OK,
      fans: ComponentStatus.OK,
      controlBoard: ComponentStatus.OK,
    } as MinerComponentStatus,
    message: "PSU error",
    alertType: alertTypes.psu,
    // 1 hour 1 minute ago
    timestamp: now.getTime() - 1000 * 60 * 60 - 1000 * 60 + 2,
  },
  {
    minerName: "C1-M05",
    minerMacAddress: "0f:04:8a:54:fa:9f",
    minerIp: "172.27.193.225",
    minerStatus: {
      hashBoards: ComponentStatus.OK,
      psu: ComponentStatus.OK,
      fans: ComponentStatus.OK,
      controlBoard: ComponentStatus.OK,
    } as MinerComponentStatus,
    message: "Pool error",
    alertType: alertTypes.pool,
    // 2 hours ago
    timestamp: now.getTime() - 1000 * 60 * 60 * 2,
  },
];
