import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

// Pairing remediation states differ in telemetry behavior:
// AUTHENTICATION_NEEDED suppresses live metrics because bad credentials block telemetry;
// DEFAULT_PASSWORD keeps telemetry flowing and is handled from Settings > Security.

export const needsAuthentication = (status: PairingStatus): boolean => status === PairingStatus.AUTHENTICATION_NEEDED;

export const needsPasswordChange = (status: PairingStatus): boolean => status === PairingStatus.DEFAULT_PASSWORD;
