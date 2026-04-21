export function formatScope(_scopeType?: string, scopeLabel?: string, scopeCount?: number): string {
  if (!scopeLabel && !scopeCount) return "\u2014";
  if (scopeLabel && scopeCount) {
    const unit = scopeCount === 1 ? "miner" : "miners";
    return `${scopeLabel} (${scopeCount} ${unit})`;
  }
  if (scopeLabel) return scopeLabel;
  if (scopeCount) {
    const unit = scopeCount === 1 ? "miner" : "miners";
    return `${scopeCount} ${unit}`;
  }
  return "\u2014";
}
