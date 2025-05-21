import { MinerComponentStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { AlertType } from "@/protoFleet/features/fleetManagement/components/AlertsModal/constants";

export type Alert = {
  minerName: string;
  minerMacAddress: string;
  minerIp: string;
  minerStatus: MinerComponentStatus;
  message: string;
  alertType: AlertType;
  timestamp: number;
};
