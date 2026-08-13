/**
 * Pure helpers for the container Pools monitoring view. Kept separate from the
 * card components so the share math is unit-testable without a DOM.
 */

export const normalizeShareCount = (value: number): number => (Number.isFinite(value) && value > 0 ? value : 0);

/**
 * Acceptance rate as a percentage of all submitted shares. Telemetry counts are
 * normalized to non-negative finite values, and an empty sample returns 0.
 */
export const getAcceptanceRate = (accepted: number, rejected: number, invalid: number): number => {
  const normalizedAccepted = normalizeShareCount(accepted);
  const total = normalizedAccepted + normalizeShareCount(rejected) + normalizeShareCount(invalid);
  if (total === 0) return 0;
  return (normalizedAccepted / total) * 100;
};

/**
 * Formats an acceptance rate for display, trimming trailing zeros so whole
 * values read cleanly (100%, 97.3%, 99.98%). Invalid values render as 0%.
 */
export const formatAcceptanceRate = (rate: number): string => {
  const normalizedRate = Number.isFinite(rate) ? Math.min(Math.max(rate, 0), 100) : 0;
  return `${parseFloat(normalizedRate.toFixed(2))}%`;
};

/**
 * Abbreviates a share/block count the way the design shows it (305, 71.9K,
 * 1.19K, 524.3K, 165.6B). Uses more precision for small mantissas so values
 * read at roughly three significant figures: a mantissa below 10 keeps two
 * decimals (1.19K), otherwise one (71.9K, 165.6B). Trailing zeros are
 * trimmed; invalid or negative telemetry is displayed as zero.
 */
export const formatShareCount = (value: number): string => {
  const normalizedValue = normalizeShareCount(value);
  const units: { threshold: number; suffix: string }[] = [
    { threshold: 1e12, suffix: "T" },
    { threshold: 1e9, suffix: "B" },
    { threshold: 1e6, suffix: "M" },
    { threshold: 1e3, suffix: "K" },
  ];
  for (const { threshold, suffix } of units) {
    if (normalizedValue >= threshold) {
      const scaled = normalizedValue / threshold;
      const digits = scaled < 10 ? 2 : 1;
      return `${parseFloat(scaled.toFixed(digits))}${suffix}`;
    }
  }
  return `${normalizedValue}`;
};
