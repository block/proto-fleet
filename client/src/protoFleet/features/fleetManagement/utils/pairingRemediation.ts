import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

// Pairing states that surface as "needs attention". They differ in telemetry and
// remediation: AUTHENTICATION_NEEDED suppresses live metrics (bad credentials block
// telemetry) and needs credentials; DEFAULT_PASSWORD keeps telemetry flowing and
// only needs a password change.

export const needsAuthentication = (status: PairingStatus): boolean => status === PairingStatus.AUTHENTICATION_NEEDED;

export const needsPasswordChange = (status: PairingStatus): boolean => status === PairingStatus.DEFAULT_PASSWORD;

export const needsCredentialRemediation = (status: PairingStatus): boolean =>
  needsAuthentication(status) || needsPasswordChange(status);

// Operator-facing remediation label for default-password devices.
export const PASSWORD_CHANGE_REQUIRED_LABEL = "Password change required";
