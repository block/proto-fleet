import { describe, expect, it } from "vitest";

import {
  getPhoneHeaderWidgetOffsetClass,
  getPhoneHeaderWidgetRowCount,
  getPhoneHeaderWidgetRowHeightClass,
  getVisibleHeaderWidgetCount,
} from "./headerWidgetLayout";

describe("headerWidgetLayout", () => {
  it("counts rollout and extends the phone ladder to five stacked widgets", () => {
    const widgetCount = getVisibleHeaderWidgetCount({
      hasDismissedSetup: true,
      hasVisibleAlertsPill: true,
      hasVisibleCurtailmentPill: true,
      hasVisibleRolloutPill: true,
      hasVisibleSchedules: true,
      hasVisibleUpdatePill: true,
    });
    const rowCount = getPhoneHeaderWidgetRowCount(widgetCount, true);

    expect(widgetCount).toBe(6);
    expect(rowCount).toBe(5);
    expect(getPhoneHeaderWidgetRowHeightClass(rowCount, true)).toBe("h-[200px]");
    expect(getPhoneHeaderWidgetOffsetClass(rowCount, true)).toBe("phone:top-[calc(theme(spacing.1)*12+200px)]");
  });
});
