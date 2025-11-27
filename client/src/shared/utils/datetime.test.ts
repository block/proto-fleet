import { describe, expect, test } from "vitest";
import { getRelativeTimeFromEpoch, getTimeFromEpoch } from "@/shared/utils/datetime";

// since epoch gets converted to local timestamp, check general format rather than exact time
const expectedTimestamp = new RegExp(/^[0-9]{2}:[0-9]{2}:[0-9]{2}$/);

describe("getTimeFromEpoch", () => {
  test("should return the formatted timestamp when epoch is provided in seconds", () => {
    const epoch = 1634567890;
    const result = getTimeFromEpoch(epoch);
    expect(result).toMatch(expectedTimestamp);
  });

  test("should return the formatted timestamp when epoch is provided in miliseconds", () => {
    const epoch = 1634567890000;
    const result = getTimeFromEpoch(epoch);
    expect(result).toMatch(expectedTimestamp);
  });

  test("should return an empty string when epoch is not provided", () => {
    const result = getTimeFromEpoch();
    expect(result).toBe("");
  });
});

describe("getRelativeTimeFromEpoch", () => {
  test("should return 'Just now' if the epoch is less than 60 seconds ago", () => {
    let epoch = Date.now() - 30 * 1000; // 30 seconds ago
    let result = getRelativeTimeFromEpoch(epoch);
    expect(result).toBe("Just now");

    epoch = Date.now() - 59 * 1000; // 59 seconds ago
    result = getRelativeTimeFromEpoch(epoch);
    expect(result).toBe("Just now");
  });

  test("should return 'Xm ago' if the epoch is less than 60 minutes ago", () => {
    let epoch = Date.now() - 60 * 1000; // 1 minute ago
    let result = getRelativeTimeFromEpoch(epoch);
    expect(result).toBe("1m ago");

    epoch = Date.now() - 30 * 60 * 1000; // 30 minutes ago
    result = getRelativeTimeFromEpoch(epoch);
    expect(result).toBe("30m ago");
  });

  test("should return 'Xh ago' if the epoch is less than 24 hours ago", () => {
    const epoch = Date.now() - 2 * 60 * 60 * 1000; // 2 hours ago
    const result = getRelativeTimeFromEpoch(epoch);
    expect(result).toBe("2hrs ago");
  });

  test("should return 'XhYm ago' if the epoch is less than 24 hours ago and has remaining minutes", () => {
    const epoch = Date.now() - (60 * 60 * 1000 + 30 * 60 * 1000); // 1 hour and 30 minutes ago
    const result = getRelativeTimeFromEpoch(epoch);
    expect(result).toBe("1h30m ago");
  });

  test("should return 'Xd ago' if the epoch is more than 24 hours ago", () => {
    const epoch = Date.now() - 2 * 24 * 60 * 60 * 1000; // 2 days ago
    const result = getRelativeTimeFromEpoch(epoch);
    expect(result).toBe("2d ago");
  });

  test("should return an empty string if the epoch is not provided", () => {
    const result = getRelativeTimeFromEpoch();
    expect(result).toBe("");
  });
});
