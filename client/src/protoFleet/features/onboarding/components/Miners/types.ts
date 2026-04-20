import { type Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";

// TODO: remove this once we have model included with Device type
export type MinerWithModel = Device & {
  model: string;
};

export type MinerWithSelected = MinerWithModel & {
  selected: boolean;
};

export type MinerWithSelectedAndAction = MinerWithSelected & {
  action?: null;
};

export type MinerDiscoveryMode = "onboarding" | "pairing";
