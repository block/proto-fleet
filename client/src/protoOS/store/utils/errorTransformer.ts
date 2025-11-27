import {
  ASIC_ERROR_CODES,
  ERROR_CODE_TO_SOURCE,
  errorMessageGenerators,
  LEGACY_TO_NUMERIC_ERROR_CODE,
  POOL_ERROR_CODES,
} from "./errorConstants";
import { NotificationError } from "@/protoOS/api/generatedApi";
import { ErrorLevel, ErrorSource, MinerError } from "@/protoOS/store/types";

/**
 * Determines the error source based on error code
 */
function getErrorSource(errorCode: string): ErrorSource {
  // Check for pool errors first (they're in the control board range)
  if (POOL_ERROR_CODES.includes(errorCode)) {
    return "POOL";
  }

  // Check if it's an ASIC error - transform to HASHBOARD for UI consistency
  // ASIC errors are always displayed as hashboard errors
  if (ASIC_ERROR_CODES.includes(errorCode)) {
    return "HASHBOARD";
  }

  // Defensive: handle unmapped string codes that somehow got through
  // This prevents them from defaulting to SYSTEM/Control board
  if (!/^\d{2}:\d{4}$/.test(errorCode)) {
    // Try to determine source from the string name
    if (errorCode.includes("Psu")) return "PSU";
    if (errorCode.includes("Fan")) return "FAN";
    if (errorCode.includes("Hashboard") || errorCode.includes("Hb")) return "HASHBOARD";
    if (errorCode.includes("Asic")) return "HASHBOARD"; // ASICs show as hashboard
    if (errorCode.includes("Pool")) return "POOL";
    if (errorCode.includes("Control") || errorCode.includes("Cb")) return "SYSTEM";

    // If we can't determine, log warning and default to SYSTEM
    console.warn(`Unmapped error code: ${errorCode} - defaulting to SYSTEM`);
    return "SYSTEM";
  }

  // Get the component type prefix (first 2 digits) for numeric codes
  const componentPrefix = errorCode.substring(0, 2);
  return ERROR_CODE_TO_SOURCE[componentPrefix] || "SYSTEM";
}

/**
 * Parse details JSON string and extract component indices
 */
function parseErrorDetails(details?: string): {
  parsedDetails: any;
  componentIndex?: number;
} {
  if (!details) {
    return { parsedDetails: null };
  }

  try {
    const parsedDetails = JSON.parse(details);
    let componentIndex: number | undefined;

    // Extract the actual error object from the wrapper
    // Details format: {"ErrorType": {...actual details...}}
    // For legacy errors, it might be {"FanSlow": {...}} or {"FanNotSpinning": {...}}
    const errorData = Object.values(parsedDetails)[0] as any;

    // If there's no wrapped data, use the parsed details directly
    // This handles cases where details might not be wrapped
    const dataToUse = errorData || parsedDetails;

    if (!dataToUse) {
      return { parsedDetails: null };
    }

    // PSU errors
    if (dataToUse.psu_bay_index !== undefined) {
      // PSU bay index is 1-based, convert to 0-based
      componentIndex = dataToUse.psu_bay_index - 1;
    } else if (dataToUse.psu_index !== undefined) {
      // PSU index is 1-based, convert to 0-based
      componentIndex = dataToUse.psu_index - 1;
    }

    // Fan errors
    if (dataToUse.fan_bay_index !== undefined) {
      // Fan bay index is 1-based, convert to 0-based
      componentIndex = dataToUse.fan_bay_index - 1;
    } else if (dataToUse.fan_id !== undefined) {
      // Fan ID is already 0-based
      componentIndex = dataToUse.fan_id;
    }

    // ASIC errors (have both ASIC index and hashboard slot)
    // When transformed to HASHBOARD, use the hashboard slot as componentIndex
    if (dataToUse.asic_index !== undefined && dataToUse.hb_slot !== undefined) {
      // For ASIC errors, componentIndex will be the hashboard slot (0-based)
      // The ASIC index is preserved in the error message
      componentIndex = dataToUse.hb_slot - 1; // Hashboard slot is 1-based, convert to 0-based
    }
    // Hashboard errors (only have hashboard slot)
    else if (dataToUse.hb_slot !== undefined) {
      // Hashboard slot is 1-based, convert to 0-based
      componentIndex = dataToUse.hb_slot - 1;
    }

    // Pool errors
    if (dataToUse.pool_id !== undefined) {
      componentIndex = dataToUse.pool_id; // Pool priority/index
    }

    return { parsedDetails: dataToUse, componentIndex };
  } catch (e) {
    console.error("Failed to parse error details:", e, details);
    return { parsedDetails: null };
  }
}

/**
 * Generate a descriptive message based on error code and details
 */
function generateErrorMessage(errorCode: string, details: any, originalMessage?: string): string {
  const generator = errorMessageGenerators[errorCode];
  if (generator) {
    return generator(details);
  }

  // Fallback to original message or generic
  return originalMessage || `Error ${errorCode}`;
}

/**
 * Transform a NotificationError from the API into our MinerError type
 */
export function transformNotificationError(apiError: NotificationError): MinerError {
  let errorCode = apiError.error_code || "00:0000";

  // Convert legacy string error codes to numeric codes
  if (LEGACY_TO_NUMERIC_ERROR_CODE[errorCode]) {
    errorCode = LEGACY_TO_NUMERIC_ERROR_CODE[errorCode];
  }

  const { parsedDetails, componentIndex } = parseErrorDetails(apiError.details);

  const source = getErrorSource(errorCode);
  const message = generateErrorMessage(errorCode, parsedDetails, apiError.message);

  return {
    errorCode,
    errorLevel: (apiError.error_level?.toUpperCase() || "ERROR") as ErrorLevel,
    insertedAt: apiError.inserted_at || Date.now(),
    expiredAt: apiError.expired_at,
    source,
    componentIndex,
    message,
  };
}

/**
 * Transform an array of NotificationErrors into MinerErrors
 */
export function transformErrors(apiErrors: NotificationError[] | undefined): MinerError[] {
  if (!apiErrors || !Array.isArray(apiErrors)) {
    return [];
  }

  return apiErrors.map(transformNotificationError);
}
