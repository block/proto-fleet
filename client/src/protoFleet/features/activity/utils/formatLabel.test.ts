import { describe, expect, it } from "vitest";

import { formatActivityFilterLabel, formatLabel } from "./formatLabel";

describe("formatLabel", () => {
  it("uses readable labels for known event types", () => {
    expect(formatLabel("login_failed")).toBe("Couldn't log in");
    expect(formatLabel("reboot")).toBe("Reboot miners");
    expect(formatLabel("set_rack_slot")).toBe("Updated rack position");
    expect(formatLabel("site.created")).toBe("Created site");
    expect(formatLabel("devices.reassigned_to_site")).toBe("Reassigned miners to site");
    expect(formatLabel("between_channel_rollout_member.moved")).toBe("Confirmed rollout lane membership");
    expect(formatLabel("between_channel_rollout_member.attention_required")).toBe("Rollout member needs attention");
    expect(formatLabel("cli_reset_password")).toBe("Break-glass password reset");
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
    expect(formatActivityFilterLabel("between_channel_rollout_member.membership_conflict")).toBe(
      "Review rollout membership change",
    );
    expect(formatActivityFilterLabel("cli_reset_password")).toBe("Break-glass password reset");
    expect(formatActivityFilterLabel("alert")).toBe("Alerts");
  });

  it("keeps curtailment lifecycle filter labels distinct from curtail commands", () => {
    expect(formatActivityFilterLabel("curtailment_started")).toBe("Curtailment started");
    expect(formatActivityFilterLabel("curtailment_started")).not.toBe(formatActivityFilterLabel("curtail"));
    expect(formatActivityFilterLabel("curtailment_admin_terminated")).toBe("Curtailment stopped");
  });
});
