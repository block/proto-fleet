interface HeaderWidgetVisibilityInput {
  hasDismissedSetup: boolean;
  hasActiveCurtailment: boolean;
  hasVisibleSchedules: boolean;
}

export function hasVisibleHeaderWidgets({
  hasDismissedSetup,
  hasActiveCurtailment,
  hasVisibleSchedules,
}: HeaderWidgetVisibilityInput): boolean {
  return hasDismissedSetup || hasActiveCurtailment || hasVisibleSchedules;
}
