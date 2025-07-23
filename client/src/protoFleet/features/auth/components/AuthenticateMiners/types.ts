import { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

// TODO change later once we have model from API
export type UnauthenticatedMiner = MinerStateSnapshot & {
  model: string;
  username: string;
  password: string;
};

export type Credentials = {
  username: string;
  password: string;
};
