import { describe, expect, it } from "vitest";

import {
  buildDateInTimeZone,
  formatDateValue,
  parseDate,
} from "@/protoFleet/features/settings/utils/scheduleDateUtils";

describe("scheduleDateUtils", () => {
  it("formats date objects as yyyy-mm-dd values", () => {
    expect(formatDateValue(new Date(2026, 3, 8))).toBe("2026-04-08");
  });

  it("rejects overflowed calendar dates", () => {
    expect(parseDate("2026-02-31")).toBeNull();
    expect(parseDate("2026-13-01")).toBeNull();
  });

  it("returns null for nonexistent spring-forward wall-clock times", () => {
    expect(buildDateInTimeZone("2026-03-08", "02:30", "America/New_York")).toBeNull();
  });
});
