import { type Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";

// TODO: remove this once we have model included with Device type
export type MinerWithModel = Device & {
  model: string;
};

export type MinerDiscoveryMode = "onboarding" | "pairing";

export type RemoteDiscoveryReadiness =
  "none" | "noNodes" | "enrollmentIncomplete" | "partialCoverage" | "discoveryNotConfigured" | "checkFailed";
