import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

// Pairing states that block telemetry until the operator acts. Both surface as
// "needs attention" and suppress live metrics, but their remediation differs:
// AUTHENTICATION_NEEDED needs credentials, DEFAULT_PASSWORD needs a password change.

export const needsAuthentication = (status: PairingStatus): boolean => status === PairingStatus.AUTHENTICATION_NEEDED;

export const needsPasswordChange = (status: PairingStatus): boolean => status === PairingStatus.DEFAULT_PASSWORD;

export const needsCredentialRemediation = (status: PairingStatus): boolean =>
  needsAuthentication(status) || needsPasswordChange(status);
