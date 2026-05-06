import type { NumericRangeValue } from "./filterValidation";

/**
 * Formats a NumericRangeValue as a chip summary.
 * - Both bounds: "50 TH/s - 200 TH/s" (range form, easier to scan)
 * - Min only:    "≥ 50 TH/s"
 * - Max only:    "≤ 200 TH/s"
 * - Neither:     "" (caller decides not to render)
 * Inclusivity is hardcoded to inclusive in v1; the chip text matches.
 */
export const formatNumericRangeCondition = (value: NumericRangeValue, unit: string): string => {
  if (value.min !== undefined && value.max !== undefined) {
    return `${value.min} ${unit} - ${value.max} ${unit}`;
  }
  if (value.min !== undefined) return `≥ ${value.min} ${unit}`;
  if (value.max !== undefined) return `≤ ${value.max} ${unit}`;
  return "";
};

type TextareaListFormatOptions = {
  /**
   * Singular noun for the entries (e.g. "subnet"). Pluralized with a trailing
   * "s" when there's more than one. Defaults to "entry"/"entries".
   */
  noun?: string;
};

/**
 * Formats a textarea-list filter (e.g. a CIDR list) as a chip summary.
 * - 0 entries: "" (caller chooses not to render)
 * - 1 entry:  the raw value (e.g. "192.168.1.0/24")
 * - >1:       "N <noun>s" (e.g. "3 subnets") — keeps the chip compact
 *             regardless of how many CIDRs the user pasted.
 */
export const formatTextareaListCondition = (values: string[], options: TextareaListFormatOptions = {}): string => {
  if (values.length === 0) return "";
  if (values.length === 1) return values[0];
  const singular = options.noun;
  if (singular === undefined) {
    return `${values.length} entries`;
  }
  // Naive pluralization: append "s". Good enough for the nouns in play
  // ("subnet", "address", "entry"). Switch to a small map if richer plurals
  // become necessary.
  return `${values.length} ${singular}s`;
};
