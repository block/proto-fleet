import { describe, expect, it } from "vitest";
import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";

import { activityEntryFromAlert, alertEntryMatchesScopes, alertsMatchFilter, mergeAlertEntries } from "./alertEntries";
import {
  type ActivityEntry,
  ActivityEntrySchema,
  ActivityFilterSchema,
} from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { buildAlertHistoryEntry } from "@/protoFleet/features/alerts/alertHistory.fixtures";

const entryAt = (eventId: string, iso: string): ActivityEntry =>
  create(ActivityEntrySchema, { eventId, createdAt: timestampFromDate(new Date(iso)) });

describe("activityEntryFromAlert", () => {
  it("maps a firing alert onto a synthetic activity entry", () => {
    const entry = activityEntryFromAlert(buildAlertHistoryEntry());

    expect(entry.eventId).toBe("alert-1");
    expect(entry.eventCategory).toBe("alert");
    expect(entry.eventType).toBe("alert_firing");
    expect(entry.description).toBe("Alert firing: Miner Offline");
    expect(entry.scopeType).toBe("device");
    expect(entry.scopeLabel).toBe("miner-1");
    expect(entry.username).toBeUndefined();
    expect(Number(entry.createdAt?.seconds)).toBe(Date.parse("2026-08-01T00:00:00Z") / 1000);
    expect(entry.metadata).toMatchObject({ severity: "critical", summary: "miner-1 has been offline for 5m" });
  });

  it("marks resolved alerts and omits the scope for redacted device names", () => {
    const entry = activityEntryFromAlert(buildAlertHistoryEntry({ status: "resolved", device_name: "" }));

    expect(entry.eventType).toBe("alert_resolved");
    expect(entry.description).toBe("Alert resolved: Miner Offline");
    expect(entry.scopeType).toBeUndefined();
    expect(entry.scopeLabel).toBeUndefined();
  });

  it("degrades a malformed received_at to an undated entry instead of throwing", () => {
    const entry = activityEntryFromAlert(buildAlertHistoryEntry({ received_at: "" }));

    expect(entry.createdAt).toBeUndefined();
  });
});

describe("alertsMatchFilter", () => {
  const filterWith = (overrides: MessageInitShape<typeof ActivityFilterSchema> = {}) =>
    create(ActivityFilterSchema, overrides);

  it("matches the unfiltered feed and an explicit Alerts selection", () => {
    expect(alertsMatchFilter(filterWith())).toBe(true);
    expect(alertsMatchFilter(filterWith({ eventTypes: ["alert", "login"] }))).toBe(true);
  });

  it("excludes alerts for user, search, or other-type filters", () => {
    expect(alertsMatchFilter(filterWith({ userIds: ["u1"] }))).toBe(false);
    expect(alertsMatchFilter(filterWith({ searchText: "reboot" }))).toBe(false);
    expect(alertsMatchFilter(filterWith({ eventTypes: ["login"] }))).toBe(false);
    expect(alertsMatchFilter(filterWith({ scopeTypes: ["rack"] }))).toBe(false);
    expect(alertsMatchFilter(filterWith({ scopeTypes: ["rack", "device"] }))).toBe(true);
  });
});

describe("alertEntryMatchesScopes", () => {
  const deviceAlert = activityEntryFromAlert(buildAlertHistoryEntry());
  const devicelessAlert = activityEntryFromAlert(buildAlertHistoryEntry({ device_name: "" }));

  it("matches device alerts against a device scope and drops device-less ones", () => {
    expect(alertEntryMatchesScopes(deviceAlert, ["device"])).toBe(true);
    expect(alertEntryMatchesScopes(devicelessAlert, ["device"])).toBe(false);
  });

  it("matches every alert when no scope is selected", () => {
    expect(alertEntryMatchesScopes(deviceAlert, [])).toBe(true);
    expect(alertEntryMatchesScopes(devicelessAlert, [])).toBe(true);
  });
});

describe("mergeAlertEntries", () => {
  const activities = [entryAt("act-new", "2026-08-01T00:00:30Z"), entryAt("act-old", "2026-08-01T00:00:10Z")];
  const alerts = [entryAt("alert-new", "2026-08-01T00:00:20Z"), entryAt("alert-old", "2026-08-01T00:00:00Z")];

  const ids = (entries: ActivityEntry[]) => entries.map((entry) => entry.eventId);

  it("interleaves both exhausted feeds newest first", () => {
    expect(ids(mergeAlertEntries(activities, false, alerts, false))).toEqual([
      "act-new",
      "alert-new",
      "act-old",
      "alert-old",
    ]);
  });

  it("holds back alerts while the activity feed has unloaded pages", () => {
    expect(ids(mergeAlertEntries(activities, true, alerts, false))).toEqual(["act-new", "alert-new", "act-old"]);
  });

  it("holds back activities while the alert feed has unloaded pages", () => {
    const withOlder = [...activities, entryAt("act-oldest", "2026-07-31T23:59:50Z")];

    expect(ids(mergeAlertEntries(withOlder, false, alerts, true))).toEqual([
      "act-new",
      "alert-new",
      "act-old",
      "alert-old",
    ]);
  });

  it("pauses at the first exhausted feed when both have more", () => {
    expect(ids(mergeAlertEntries(activities, true, alerts, true))).toEqual(["act-new", "alert-new", "act-old"]);
  });

  it("keeps alert cursor order when timestamps invert within the feed", () => {
    const inverted = [entryAt("alert-a", "2026-08-01T00:00:20Z"), entryAt("alert-b", "2026-08-01T00:00:25Z")];

    expect(ids(mergeAlertEntries([], false, inverted, false))).toEqual(["alert-a", "alert-b"]);
  });

  it("only appends when a feed loads more pages", () => {
    const partial = mergeAlertEntries(activities, false, alerts.slice(0, 1), true);
    const full = mergeAlertEntries(activities, false, alerts, false);

    expect(ids(full).slice(0, partial.length)).toEqual(ids(partial));
  });

  it("returns activities untouched when the alert feed is exhausted and empty", () => {
    expect(mergeAlertEntries(activities, true, [], false)).toBe(activities);
  });

  it("holds everything back while an empty alert feed still has pages", () => {
    expect(mergeAlertEntries(activities, false, [], true)).toEqual([]);
  });
});
