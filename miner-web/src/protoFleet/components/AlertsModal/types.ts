import { AlertType } from "@/protoFleet/components/AlertsModal/constants";
import { MinerStatus } from "@/protoFleet/components/MinerList/types";

export type Alert = {
  minerName: string;
  minerMacAddress: string;
  minerIp: string;
  minerStatus: MinerStatus;
  message: string;
  alertType: AlertType;
  timestamp: number;
};
