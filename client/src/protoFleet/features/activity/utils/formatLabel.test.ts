import { describe, expect, it } from "vitest";

import { formatActivityFilterLabel, formatLabel } from "./formatLabel";

describe("formatLabel", () => {
  it("uses readable labels for known event types", () => {
    expect(formatLabel("login_failed")).toBe("Couldn't log in");
    expect(formatLabel("reboot")).toBe("Reboot miners");
    expect(formatLabel("set_rack_slot")).toBe("Updated rack position");
    expect(formatLabel("site.created")).toBe("Created site");
    expect(formatLabel("devices.reassigned_to_site")).toBe("Reassigned miners to site");
  });

  it("formats completed event types using the base event label", () => {
    expect(formatLabel("reboot.completed")).toBe("Reboot miners");
  });

  it("falls back to sentence-case labels without exposing backend separators", () => {
    expect(formatLabel("future_api_event.created")).toBe("Future API event created");
  });

  it("uses action-oriented labels for activity type filters", () => {
    expect(formatActivityFilterLabel("login")).toBe("Log in");
    expect(formatActivityFilterLabel("set_rack_slot")).toBe("Update rack position");
    expect(formatActivityFilterLabel("site.created")).toBe("Create site");
    expect(formatActivityFilterLabel("set_power_target.completed")).toBe("Update power target");
  });
});
