import { type PairedDevice } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { type MinerStatus } from "@/protoFleet/features/fleetManagement/types";

export type Miner = PairedDevice & {
  name?: string;
  status?: MinerStatus;
  hashrate?: { time: number; hashrate: number }[];
  efficiency?: number;
  powerUsage?: number;
  temperature?: number;
  ip?: string;
};
