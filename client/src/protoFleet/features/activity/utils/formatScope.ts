export function formatScope(scopeType?: string, scopeLabel?: string, scopeCount?: number): string {
  const singular = scopeType === "container" ? "container" : "miner";
  const unit = scopeCount === 1 ? singular : `${singular}s`;
  if (!scopeLabel && !scopeCount) return "\u2014";
  if (scopeLabel && scopeCount) {
    return `${scopeLabel} (${scopeCount} ${unit})`;
  }
  if (scopeLabel) return scopeLabel;
  if (scopeCount) {
    return `${scopeCount} ${unit}`;
  }
  return "\u2014";
}
