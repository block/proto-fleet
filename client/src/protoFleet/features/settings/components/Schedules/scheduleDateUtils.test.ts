import { describe, expect, it } from "vitest";

import { buildDateInTimeZone } from "@/protoFleet/features/settings/components/Schedules/scheduleDateUtils";

describe("scheduleDateUtils", () => {
  it("returns null for nonexistent spring-forward wall-clock times", () => {
    expect(buildDateInTimeZone("2026-03-08", "02:30", "America/New_York")).toBeNull();
  });
});
