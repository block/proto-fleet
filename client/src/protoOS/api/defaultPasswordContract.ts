import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

// Mirrors the firmware default-password contract encoded in
// `server/sdk/v1/errors.go` (see TestDefaultPasswordContract). If the markers
// below drift from the SDK constants, default-password detection silently
// breaks on the client. Keep them in sync; the Go contract test is authoritative.
export const DEFAULT_PASSWORD_CODE = "default_password_active";
export const DEFAULT_PASSWORD_MESSAGE_MARKER = "default password must be changed";

const readCode = (error: ErrorProps): string => (error?.error?.error?.code ?? error?.error?.code ?? "").toLowerCase();

const readMessage = (error: ErrorProps): string =>
  (error?.error?.error?.message ?? error?.error?.message ?? "").toLowerCase();

// When firmware returns a plain-text 403 body (e.g. "default password must be
// changed"), the generated API client's JSON parse fails and stores the raw
// SyntaxError in error.error — which embeds the body in its own message
// (e.g. `Unexpected token 'd', "default pa"... is not valid JSON`). Stringifying
// that fallback lets the marker still match.
const readRawErrorText = (error: ErrorProps): string => {
  const raw = error?.error;
  if (!raw || typeof raw !== "object") return "";
  if (raw instanceof Error) return raw.message.toLowerCase();
  return "";
};

export const isDefaultPasswordActiveError = (error: ErrorProps): boolean => {
  if (error?.status !== 403) return false;
  const code = readCode(error);
  if (code === DEFAULT_PASSWORD_CODE) return true;
  const haystack = `${readMessage(error)} ${readRawErrorText(error)}`;
  return haystack.includes(DEFAULT_PASSWORD_MESSAGE_MARKER) || haystack.includes(DEFAULT_PASSWORD_CODE);
};
