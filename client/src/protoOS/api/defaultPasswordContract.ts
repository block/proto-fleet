import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

// Mirrors the firmware #3269 default-password contract encoded in
// `server/sdk/v1/errors.go` (see TestDefaultPasswordContract). If the markers
// below drift from the SDK constants, default-password detection silently
// breaks on the client. Keep them in sync; the Go contract test is authoritative.
export const DEFAULT_PASSWORD_CODE = "default_password_active";
export const DEFAULT_PASSWORD_MESSAGE_MARKER = "default password must be changed";

const readCode = (error: ErrorProps): string => (error?.error?.error?.code ?? error?.error?.code ?? "").toLowerCase();

const readMessage = (error: ErrorProps): string =>
  (error?.error?.error?.message ?? error?.error?.message ?? "").toLowerCase();

export const isDefaultPasswordActiveError = (error: ErrorProps): boolean => {
  if (error?.status !== 403) return false;
  const code = readCode(error);
  const message = readMessage(error);
  return (
    code === DEFAULT_PASSWORD_CODE ||
    message.includes(DEFAULT_PASSWORD_MESSAGE_MARKER) ||
    message.includes(DEFAULT_PASSWORD_CODE)
  );
};
