// Site-specific display helpers. The general telemetry formatters
// (hashrate / efficiency / power) live in `@/shared/utils/telemetryFormat`
// since they're shared by every rollup surface — sites, buildings,
// future rack/device-set cards. Only the location string is genuinely
// site-shaped, so it stays here.

export const formatLocation = (city: string, state: string): string | null => {
  const c = city.trim();
  const s = state.trim();
  if (c && s) return `${c}, ${s}`;
  if (c) return c;
  if (s) return s;
  return null;
};
