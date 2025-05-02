import { type PairedDevice } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { type StatusCircleStatus } from "@/shared/components/StatusCircle/constants";

export type MinerStatus = {
  hashboard: StatusCircleStatus;
  asic: StatusCircleStatus;
  fans: StatusCircleStatus;
  cb: StatusCircleStatus;

  // TODO: these will probably be derived from the above
  hashing: boolean;
  offline: boolean;
  asleep: boolean;
  broken: boolean;
};

export type MinerStatusKey = keyof MinerStatus;

export type Miner = PairedDevice & {
  name?: string;
  status?: MinerStatus;
  hashrate?: { time: number; hashrate: number }[];
  efficiency?: number;
  powerUsage?: number;
  temperature?: number;
  ip?: string;
};
