import { AlertType } from "@/protoFleet/features/fleetManagement/components/AlertsModal/constants";
import { MinerStatus } from "@/protoFleet/features/fleetManagement/types";

export type Alert = {
  minerName: string;
  minerMacAddress: string;
  minerIp: string;
  minerStatus: MinerStatus;
  message: string;
  alertType: AlertType;
  timestamp: number;
};
