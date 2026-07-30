type ParsedNumericField = { parsed?: number; error?: string };

interface OptionalUint32FieldOptions {
  label: string;
  max: number;
}

export const curtailmentNumericFieldLimits = {
  maxDurationSec: 604800,
  curtailBatchSize: 10000,
  curtailBatchIntervalSec: 3600,
  restoreBatchSize: 10000,
  restoreIntervalSec: 3600,
  fanDelaySec: 3600,
  minDurationSec: 2147483647,
} as const;

export const immediateRestoreBatchSize = 0;
export const immediateRestoreBatchSizeInputValue = String(immediateRestoreBatchSize);

export function parseOptionalUint32Field(value: string, options: OptionalUint32FieldOptions): ParsedNumericField {
  const trimmed = value.trim();
  if (trimmed === "") {
    return {};
  }

  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed) || !Number.isInteger(parsed)) {
    return { error: `Enter ${options.label} as a whole number.` };
  }

  if (parsed < 0) {
    return { error: `Enter ${options.label} of 0 or more.` };
  }

  if (parsed > options.max) {
    return { error: `Enter ${options.label} of ${options.max.toLocaleString()} or less.` };
  }

  return { parsed };
}

export function getOptionalUint32Setting(value: string, options: OptionalUint32FieldOptions): number {
  const parsedField = parseOptionalUint32Field(value, options);
  if (parsedField.error) {
    throw new Error(parsedField.error);
  }

  return parsedField.parsed ?? 0;
}
