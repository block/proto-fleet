import { ErrorListResponse, NotificationError } from "@/protoOS/api/generatedApi";
import { ErrorSource, MinerError } from "@/protoOS/store/types";

/**
 * Map API source values to our ErrorSource type
 */
const SOURCE_MAP: Record<string, ErrorSource> = {
  rig: "RIG",
  fan: "FAN",
  psu: "PSU",
  hashboard: "HASHBOARD",
};

/**
 * Transform a NotificationError from the API into our MinerError type
 * Simple direct field mapping with minimal transformation
 */
export function transformNotificationError(apiError: NotificationError): MinerError {
  // Use the source map with a fallback to "RIG" for unknown sources
  const source = apiError.source ? (SOURCE_MAP[apiError.source] ?? "RIG") : "RIG";

  return {
    errorCode: apiError.error_code || "",
    timestamp: apiError.timestamp, // Keep optional, no default value
    source,
    slot: apiError.slot,
    message: apiError.message || `Error ${apiError.error_code}`,
  };
}

/**
 * Transform an array of NotificationErrors into MinerErrors
 */
export function transformErrors(response: ErrorListResponse | undefined): MinerError[] {
  return response?.map(transformNotificationError) || [];
}
