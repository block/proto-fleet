interface HeaderWidgetVisibility {
  hasDismissedSetup: boolean;
  hasVisibleCurtailmentPill: boolean;
  hasVisibleFirmwareRolloutPill: boolean;
  hasVisibleSchedules: boolean;
}

export const PHONE_HEADER_WIDGET_ROW_OFFSET_CLASS = "phone:top-[calc(theme(spacing.1)*12+40px)]";
export const PHONE_HEADER_WIDGET_STACK_TWO_OFFSET_CLASS = "phone:top-[calc(theme(spacing.1)*12+80px)]";
export const PHONE_HEADER_WIDGET_STACK_THREE_OFFSET_CLASS = "phone:top-[calc(theme(spacing.1)*12+120px)]";
export const PHONE_HEADER_WIDGET_HIDDEN_OFFSET_CLASS = "phone:top-[calc(theme(spacing.1)*12)]";
export const PHONE_HEADER_WIDGET_ROW_HEIGHT_CLASS = "h-[40px]";
export const PHONE_HEADER_WIDGET_STACK_TWO_HEIGHT_CLASS = "h-[80px]";
export const PHONE_HEADER_WIDGET_STACK_THREE_HEIGHT_CLASS = "h-[120px]";

export function getVisibleHeaderWidgetCount({
  hasDismissedSetup,
  hasVisibleCurtailmentPill,
  hasVisibleFirmwareRolloutPill,
  hasVisibleSchedules,
}: HeaderWidgetVisibility): number {
  return (
    Number(hasVisibleCurtailmentPill) +
    Number(hasVisibleFirmwareRolloutPill) +
    Number(hasVisibleSchedules) +
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

export function getPhoneHeaderWidgetRowHeightClass(widgetCount: number, stackWidgets: boolean): string {
  if (!stackWidgets) {
    return PHONE_HEADER_WIDGET_ROW_HEIGHT_CLASS;
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

  return widgetCount > 2 ? PHONE_HEADER_WIDGET_STACK_THREE_OFFSET_CLASS : PHONE_HEADER_WIDGET_STACK_TWO_OFFSET_CLASS;
}
