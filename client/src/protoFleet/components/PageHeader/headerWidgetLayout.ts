interface HeaderWidgetVisibility {
  hasDismissedSetup: boolean;
  hasVisibleAlertsPill: boolean;
  hasVisibleUpdatePill?: boolean;
  hasVisibleCurtailmentPill: boolean;
  hasVisibleRolloutPill?: boolean;
  hasVisibleSchedules: boolean;
}

export const PHONE_HEADER_WIDGET_ROW_OFFSET_CLASS = "phone:top-[calc(theme(spacing.1)*12+40px)]";
export const PHONE_HEADER_WIDGET_STACK_TWO_OFFSET_CLASS = "phone:top-[calc(theme(spacing.1)*12+80px)]";
export const PHONE_HEADER_WIDGET_STACK_THREE_OFFSET_CLASS = "phone:top-[calc(theme(spacing.1)*12+120px)]";
export const PHONE_HEADER_WIDGET_STACK_FOUR_OFFSET_CLASS = "phone:top-[calc(theme(spacing.1)*12+160px)]";
export const PHONE_HEADER_WIDGET_HIDDEN_OFFSET_CLASS = "phone:top-[calc(theme(spacing.1)*12)]";
export const PHONE_HEADER_WIDGET_ROW_HEIGHT_CLASS = "h-[40px]";
export const PHONE_HEADER_WIDGET_STACK_TWO_HEIGHT_CLASS = "h-[80px]";
export const PHONE_HEADER_WIDGET_STACK_THREE_HEIGHT_CLASS = "h-[120px]";
export const PHONE_HEADER_WIDGET_STACK_FOUR_HEIGHT_CLASS = "h-[160px]";

export function getVisibleHeaderWidgetCount({
  hasDismissedSetup,
  hasVisibleAlertsPill,
  hasVisibleUpdatePill = false,
  hasVisibleCurtailmentPill,
  hasVisibleRolloutPill = false,
  hasVisibleSchedules,
}: HeaderWidgetVisibility): number {
  return (
    Number(hasVisibleAlertsPill) +
    Number(hasVisibleCurtailmentPill) +
    Number(hasVisibleSchedules) +
    Number(hasVisibleRolloutPill) +
    Number(hasVisibleUpdatePill) +
    Number(hasDismissedSetup)
  );
}

export function shouldStackPhoneHeaderWidgets(widgetCount: number): boolean {
  return widgetCount > 2;
}

export function shouldInlineFirstPhoneHeaderWidget(widgetCount: number): boolean {
  return widgetCount > 0;
}

export function getPhoneHeaderWidgetRowCount(widgetCount: number, inlineFirstWidget: boolean): number {
  if (!inlineFirstWidget) {
    return widgetCount;
  }

  return Math.max(widgetCount - 1, 0);
}

// One rung per stacked row. Adding a widget kind raises the row count, so the ladder has to grow with it.
export function getPhoneHeaderWidgetRowHeightClass(widgetCount: number, stackWidgets: boolean): string {
  if (!stackWidgets) {
    return PHONE_HEADER_WIDGET_ROW_HEIGHT_CLASS;
  }

  if (widgetCount > 3) {
    return PHONE_HEADER_WIDGET_STACK_FOUR_HEIGHT_CLASS;
  }

  return widgetCount > 2 ? PHONE_HEADER_WIDGET_STACK_THREE_HEIGHT_CLASS : PHONE_HEADER_WIDGET_STACK_TWO_HEIGHT_CLASS;
}

export function getPhoneHeaderWidgetOffsetClass(widgetCount: number, stackWidgets: boolean): string {
  if (widgetCount === 0) {
    return PHONE_HEADER_WIDGET_HIDDEN_OFFSET_CLASS;
  }

  if (!stackWidgets) {
    return PHONE_HEADER_WIDGET_ROW_OFFSET_CLASS;
  }

  if (widgetCount > 3) {
    return PHONE_HEADER_WIDGET_STACK_FOUR_OFFSET_CLASS;
  }

  return widgetCount > 2 ? PHONE_HEADER_WIDGET_STACK_THREE_OFFSET_CLASS : PHONE_HEADER_WIDGET_STACK_TWO_OFFSET_CLASS;
}
